package gui

import (
	"context"
	"errors"
	"strings"

	"github.com/winnstorm/gabitotl/internal/credstore"
	"github.com/winnstorm/gabitotl/internal/vcshosts"
)

// Self-hosted servers (docs/DECISIONS.md D60): a non-secret registry of host, platform
// and web URL in ~/.gabitotl/vcs-hosts.json, with the token in the credential store.

// ServerInfo is a registered self-hosted server. It never carries the token.
type ServerInfo struct {
	Host     string `json:"host"`
	Platform string `json:"platform"` // bitbucket-server | github | gitlab
	URL      string `json:"url"`
	Stored   bool   `json:"stored"`
}

func (s *Service) registry() (vcshosts.Registry, error) {
	if s.d.Servers == nil {
		return nil, ErrNotAvailable
	}
	home, err := s.home()
	if err != nil {
		return nil, err
	}
	return s.d.Servers(home), nil
}

// serverEntry is the registry entry of host; a missing or unreadable registry is "none".
func (s *Service) serverEntry(host string) (vcshosts.Entry, bool) {
	reg, err := s.registry()
	if err != nil {
		return vcshosts.Entry{}, false
	}
	return vcshosts.Lookup(reg, host)
}

// Servers lists the registered self-hosted servers and whether a token is stored for each.
func (s *Service) Servers() ([]ServerInfo, error) {
	reg, err := s.registry()
	if err != nil {
		return nil, err
	}
	st, err := s.credStore()
	if err != nil {
		return nil, err
	}
	es, err := reg.List()
	if err != nil {
		return nil, err
	}
	out := make([]ServerInfo, 0, len(es))
	for _, e := range es {
		_, ok, err := st.Get(e.Host)
		if err != nil {
			return nil, err
		}
		out = append(out, ServerInfo{Host: e.Host, Platform: e.Platform, URL: e.URL, Stored: ok})
	}
	return out, nil
}

// SaveServer verifies token against the server at url and, only when it is
// accepted, registers the server and stores the token. An empty token re-checks the
// one already stored for that host. A rejected or unreachable check writes nothing.
func (s *Service) SaveServer(ctx context.Context, platform, url, token string) (CredentialResult, error) {
	reg, err := s.registry()
	if err != nil {
		return CredentialResult{}, err
	}
	st, err := s.credStore()
	if err != nil {
		return CredentialResult{}, err
	}
	if s.d.VerifyToken == nil {
		return CredentialResult{}, ErrNotAvailable
	}
	res := CredentialResult{Platform: platform, Status: CredInvalid}
	if !vcshosts.ValidPlatform(platform) {
		res.Detail = "unknown platform"
		return res, nil
	}
	base, host, err := vcshosts.ParseBaseURL(url)
	if err != nil {
		res.Detail = "the server URL is not valid"
		return res, nil
	}
	res.Host = host
	reused := false
	switch {
	case strings.TrimSpace(token) == "":
		stored, ok, gerr := st.Get(host)
		if gerr != nil || !ok {
			res.Detail = "there is no token to check: paste one"
			return res, nil
		}
		token, reused = stored, true
	case !credstore.ValidToken(token):
		res.Detail = "the token must be a single non-empty line"
		return res, nil
	}
	token = strings.TrimSpace(token)
	if s.verifyInto(ctx, &res, platform, host, base, token) {
		return res, nil
	}
	if !reused {
		if err := st.Set(host, token); err != nil {
			if errors.Is(err, credstore.ErrTokenTooLong) {
				res.Status, res.Detail = CredInvalid, "the token is too long for the credential store"
				return res, nil
			}
			return res, err
		}
	}
	if err := reg.Put(vcshosts.Entry{Host: host, Platform: platform, URL: base}); err != nil {
		if !reused {
			_ = st.Delete(host)
		}
		return res, err
	}
	res.Status, res.Stored = CredSaved, true
	return res, nil
}

// DeleteServer unregisters host and removes its stored token.
func (s *Service) DeleteServer(host string) error {
	reg, err := s.registry()
	if err != nil {
		return err
	}
	st, err := s.credStore()
	if err != nil {
		return err
	}
	if err := st.Delete(host); err != nil {
		return err
	}
	return reg.Delete(host)
}

// TestCredential asks the platform whether the STORED token of host is still
// accepted (a public platform or a registered server). Status is CredOK,
// CredRejected, CredUnreachable or CredInvalid (no token, unknown host).
func (s *Service) TestCredential(ctx context.Context, platform, host string) (CredentialResult, error) {
	st, err := s.credStore()
	if err != nil {
		return CredentialResult{}, err
	}
	if s.d.VerifyToken == nil {
		return CredentialResult{}, ErrNotAvailable
	}
	h := credstore.NormalizeHost(host)
	if h == "" {
		for _, p := range platformHosts {
			if p.Platform == platform {
				h = p.Host
			}
		}
	}
	res := CredentialResult{Platform: platform, Host: h, Status: CredInvalid}
	base := ""
	if e, ok := s.serverEntry(h); ok {
		if e.Platform != platform {
			res.Detail = "unknown platform"
			return res, nil
		}
		base = e.URL
	} else {
		public := false
		for _, p := range platformHosts {
			public = public || p.Platform == platform && p.Host == h
		}
		if !public {
			res.Detail = "unknown server"
			return res, nil
		}
	}
	token, ok, err := st.Get(h)
	if err != nil {
		return res, err
	}
	if !ok {
		res.Detail = "no token stored for this host"
		return res, nil
	}
	res.Stored = true
	if s.verifyInto(ctx, &res, platform, h, base, token) {
		return res, nil
	}
	res.Status = CredOK
	return res, nil
}
