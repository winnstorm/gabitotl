package gui

import (
	"context"
	"errors"
	"strings"

	"github.com/winnstorm/gabitotl/internal/credstore"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// Global delivery defaults and platform credentials (docs/DECISIONS.md D56). None
// of this needs a project: the install is global. A token goes straight into the
// OS credential store and is never returned, logged or written to a Markdown file.

// GlobalDelivery is the user's delivery defaults, the layer under every project.
type GlobalDelivery struct {
	Automation   string `json:"automation"` // all | until-pr | ask | custom
	MergeMethod  string `json:"mergeMethod"`
	DeleteBranch bool   `json:"deleteBranch"`
	Draft        bool   `json:"draft"`
	Saved        bool   `json:"saved"` // the global file exists
}

// GlobalDeliveryAnswers are the answers to change; an empty or nil field keeps the value.
type GlobalDeliveryAnswers struct {
	Automation   string `json:"automation"`
	MergeMethod  string `json:"mergeMethod"`
	DeleteBranch *bool  `json:"deleteBranch"`
	Draft        *bool  `json:"draft"`
}

// Credential states returned by SaveCredential.
const (
	CredSaved       = "saved"       // verified and stored
	CredRejected    = "rejected"    // the platform refused the token; nothing stored
	CredUnreachable = "unreachable" // the platform could not be asked; nothing stored
	CredInvalid     = "invalid"     // the platform, host or token is not acceptable; nothing stored
)

// CredentialInfo says whether a token is stored for a platform host. It never
// carries the token.
type CredentialInfo struct {
	Platform string `json:"platform"`
	Host     string `json:"host"`
	Stored   bool   `json:"stored"`
}

// CredentialResult is the outcome of SaveCredential; Detail is secret-free.
type CredentialResult struct {
	Platform string `json:"platform"`
	Host     string `json:"host"`
	Status   string `json:"status"`
	Stored   bool   `json:"stored"`
	Detail   string `json:"detail,omitempty"`
}

// platformHosts are the public hosts the credentials screen always lists.
var platformHosts = []CredentialInfo{
	{Platform: "github", Host: "github.com"}, {Platform: "gitlab", Host: "gitlab.com"}, {Platform: "bitbucket", Host: "bitbucket.org"},
}

func (s *Service) home() (string, error) {
	env, err := s.env("")
	if err != nil {
		return "", err
	}
	return env.Home, nil
}

func (s *Service) credStore() (credstore.Store, error) {
	if s.d.Credentials == nil {
		return nil, ErrNotAvailable
	}
	home, err := s.home()
	if err != nil {
		return nil, err
	}
	return s.d.Credentials(home), nil
}

func globalDeliveryOf(c gabitotl.VCSConfig, saved bool) GlobalDelivery {
	return GlobalDelivery{Automation: c.AutomationLevel(), MergeMethod: c.MergeMethod, DeleteBranch: c.DeleteBranch, Draft: c.DraftPRs, Saved: saved}
}

// GlobalDelivery reads the user's delivery defaults (the built-in ones before any are saved).
func (s *Service) GlobalDelivery() (GlobalDelivery, error) {
	home, err := s.home()
	if err != nil {
		return GlobalDelivery{}, err
	}
	c, ok, err := gabitotl.LoadGlobalVCS(home)
	if err != nil {
		return GlobalDelivery{}, err
	}
	return globalDeliveryOf(c, ok), nil
}

// SetGlobalDelivery saves the defaults into ~/.gabitotl/vcs.md. The file holds
// settings only; a project's own vcs.md still wins.
func (s *Service) SetGlobalDelivery(a GlobalDeliveryAnswers) (GlobalDelivery, error) {
	home, err := s.home()
	if err != nil {
		return GlobalDelivery{}, err
	}
	c, err := gabitotl.SaveGlobalDelivery(home, gabitotl.DeliveryAnswers{
		Automation: a.Automation, MergeMethod: a.MergeMethod, DeleteBranch: a.DeleteBranch, Draft: a.Draft,
	})
	if err != nil {
		return GlobalDelivery{}, err
	}
	return globalDeliveryOf(c, true), nil
}

// Credentials lists the public platform hosts and whether a token is stored for each.
func (s *Service) Credentials() ([]CredentialInfo, error) {
	st, err := s.credStore()
	if err != nil {
		return nil, err
	}
	out := make([]CredentialInfo, 0, len(platformHosts))
	for _, h := range platformHosts {
		_, ok, err := st.Get(h.Host)
		if err != nil {
			return nil, err
		}
		h.Stored = ok
		out = append(out, h)
	}
	return out, nil
}

// SaveCredential verifies token against the platform and, only when it is
// accepted, stores it for host (empty means the platform's public host). The
// result never carries the token; a rejected or unreachable check stores nothing.
func (s *Service) SaveCredential(ctx context.Context, platform, host, token string) (CredentialResult, error) {
	st, err := s.credStore()
	if err != nil {
		return CredentialResult{}, err
	}
	if s.d.VerifyToken == nil {
		return CredentialResult{}, ErrNotAvailable
	}
	host = strings.TrimSpace(host)
	if host == "" {
		for _, h := range platformHosts {
			if h.Platform == platform {
				host = h.Host
			}
		}
	}
	res := CredentialResult{Platform: platform, Host: credstore.NormalizeHost(host), Status: CredInvalid}
	known := false
	for _, h := range platformHosts {
		known = known || h.Platform == platform
	}
	switch {
	case !known:
		res.Detail = "unknown platform"
		return res, nil
	case !credstore.ValidHost(host):
		res.Detail = "invalid host"
		return res, nil
	case !credstore.ValidToken(token):
		res.Detail = "the token must be a single non-empty line"
		return res, nil
	}
	// The platform is asked at the host as typed (port included); the store key drops the port.
	ok, detail, err := s.d.VerifyToken(ctx, platform, strings.ToLower(host), strings.TrimSpace(token))
	switch {
	case err != nil:
		res.Status, res.Detail = CredUnreachable, "could not check the token"
		var perr interface{ Timeout() bool }
		if errors.As(err, &perr) && perr.Timeout() {
			res.Detail = "the platform did not answer in time"
		}
		return res, nil
	case !ok:
		res.Status, res.Detail = CredRejected, detail
		return res, nil
	}
	if err := st.Set(res.Host, token); err != nil {
		if errors.Is(err, credstore.ErrTokenTooLong) {
			res.Status, res.Detail = CredInvalid, "the token is too long for the credential store"
			return res, nil
		}
		return res, err
	}
	res.Status, res.Stored = CredSaved, true
	return res, nil
}

// DeleteCredential removes the stored token of host.
func (s *Service) DeleteCredential(host string) error {
	st, err := s.credStore()
	if err != nil {
		return err
	}
	return st.Delete(host)
}
