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
	// SSHHosts are the other host names the server's Git remotes use.
	SSHHosts []string `json:"sshHosts,omitempty"`
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

// serverEntry is the registry entry whose host (not an SSH alias: the credential
// key) is host; a missing registry is "none", a corrupt one is an error.
func (s *Service) serverEntry(host string) (vcshosts.Entry, bool, error) {
	reg, err := s.registry()
	if err != nil {
		return vcshosts.Entry{}, false, nil
	}
	e, ok, err := vcshosts.LookupErr(reg, host)
	if !ok || e.Host != credstore.NormalizeHost(host) {
		return vcshosts.Entry{}, false, err
	}
	return e, true, nil
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
		out = append(out, ServerInfo{Host: e.Host, Platform: e.Platform, URL: e.URL, Stored: ok, SSHHosts: e.SSHHosts})
	}
	return out, nil
}

// SaveServer verifies token against the server at url and, only when it is
// accepted, registers the server and stores the token. An empty token re-checks the
// one already stored for that host. A rejected or unreachable check writes nothing.
// sshHosts are other host names the server's Git remotes use. A host name that
// already belongs to a different server (credentials are keyed by host name, without
// the port) answers CredConflict until replace is true; a replacement never reuses
// the old server's token. Plain http is refused unless the host is loopback.
func (s *Service) SaveServer(ctx context.Context, platform, url, token string, sshHosts []string, replace bool) (CredentialResult, error) {
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
		res.Detail, res.Code = "the server URL is not valid", "invalid_url"
		if errors.Is(err, vcshosts.ErrInsecureHTTP) {
			res.Detail, res.Code = "plain http is not allowed: use https://", "insecure_http"
		}
		return res, nil
	}
	res.Host = host
	entry, nerr := vcshosts.Normalize(vcshosts.Entry{Platform: platform, URL: base, SSHHosts: sshHosts})
	if nerr != nil {
		res.Detail, res.Code = "the SSH host name is not valid", "invalid_url"
		return res, nil
	}
	existing, rerr := reg.List()
	if rerr != nil {
		return res, rerr
	}
	conflict := vcshosts.HasConflict(existing, entry)
	if conflict && !replace {
		res.Status, res.Code = CredConflict, "host_conflict"
		res.Detail = "this host name already belongs to a different server"
		return res, nil
	}
	reused := false
	switch {
	case strings.TrimSpace(token) == "":
		stored, ok, gerr := st.Get(host)
		if gerr != nil || !ok || conflict { // the old server's token must not go to a new one
			res.Detail, res.Code = "there is no token to check: paste one", "not_stored"
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
	if err := reg.Put(entry, vcshosts.PutOptions{Replace: replace}); err != nil {
		if !reused {
			_ = st.Delete(host)
		}
		if errors.Is(err, vcshosts.ErrHostConflict) { // another process registered it meanwhile
			res.Status, res.Code, res.Detail = CredConflict, "host_conflict", "this host name already belongs to a different server"
			return res, nil
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
	e, ok, serr := s.serverEntry(h)
	if serr != nil {
		return res, serr
	}
	if ok {
		if e.Platform != platform {
			res.Detail, res.Code = "unknown platform", "unknown_host"
			return res, nil
		}
		base = e.URL
	} else {
		public := false
		for _, p := range platformHosts {
			public = public || p.Platform == platform && p.Host == h
		}
		if !public {
			res.Detail, res.Code = "unknown server", "unknown_host"
			return res, nil
		}
	}
	token, ok, err := st.Get(h)
	if err != nil {
		return res, err
	}
	if !ok {
		res.Detail, res.Code = "no token stored for this host", "not_stored"
		return res, nil
	}
	res.Stored = true
	if s.verifyInto(ctx, &res, platform, h, base, token) {
		return res, nil
	}
	res.Status = CredOK
	return res, nil
}
