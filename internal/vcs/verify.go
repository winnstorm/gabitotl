package vcs

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// VerifyOptions tune VerifyToken; the zero value checks the real platforms.
type VerifyOptions struct {
	HTTP *http.Client
	// APIBase replaces the API root of any platform (tests); empty derives it from the host.
	APIBase string
	// BaseURL is the web base URL of a self-hosted server (from the registry): the
	// only source of the API root for bitbucket-server, and of GitHub Enterprise /
	// self-managed GitLab when set.
	BaseURL string
}

// DefaultHost is the public host of a platform.
func DefaultHost(platform string) string {
	switch platform {
	case "github":
		return "github.com"
	case "gitlab":
		return "gitlab.com"
	case "bitbucket":
		return "bitbucket.org"
	}
	return ""
}

// VerifyToken asks the platform API whether token is accepted, with no project
// involved. GitHub and GitLab are checked with the token as a bearer on /user
// (GitHub Enterprise and self-managed GitLab hosts use their own API paths);
// Bitbucket Cloud goes through the same client the delivery commands use. It
// returns AuthLoggedIn or AuthNotLoggedIn; transport failures are errors. The
// token is never part of an error or of the returned state.
func VerifyToken(ctx context.Context, platform, host, token string, o VerifyOptions) (AuthState, error) {
	token = strings.TrimSpace(token)
	st := AuthState{Provider: platform}
	if token == "" {
		st.Status, st.Detail = AuthNotLoggedIn, "the token is empty"
		return st, nil
	}
	if host == "" {
		host = DefaultHost(platform)
	}
	// Never follow a redirect: the token header must not be forwarded to another host.
	client := &http.Client{Timeout: 20 * time.Second}
	if o.HTTP != nil {
		c := *o.HTTP
		client = &c
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	o.HTTP = client
	var url, header, value string
	switch platform {
	case "bitbucket":
		h, err := HostFor("bitbucket", HostOptions{Runner: noRunner{}, HTTP: o.HTTP, BitbucketAPI: o.APIBase,
			Credentials: func() (Credential, error) { return ParseCredential(token), nil }})
		if err != nil {
			return st, err
		}
		return h.AuthStatus(ctx, "")
	case "bitbucket-server":
		if o.BaseURL == "" {
			st.Status = AuthUnsupported
			return st, fmt.Errorf("%w: a Bitbucket server needs its URL", ErrUnsupported)
		}
		user, ok, err := checkBitbucketServer(ctx, client, strings.TrimRight(o.BaseURL, "/"), host, token)
		if err != nil {
			return st, err
		}
		if !ok {
			st.Status, st.Detail = AuthNotLoggedIn, "the token was rejected by "+host
			return st, nil
		}
		st.Status, st.User, st.Detail = AuthLoggedIn, user, "signed in as "+user
		return st, nil
	case "github":
		base := o.APIBase
		switch {
		case base != "":
		case o.BaseURL != "":
			base = strings.TrimRight(o.BaseURL, "/") + "/api/v3"
		case host == "github.com":
			base = "https://api.github.com"
		default:
			base = "https://" + host + "/api/v3"
		}
		url, header, value = strings.TrimRight(base, "/")+"/user", "Authorization", "Bearer "+token
	case "gitlab":
		base := o.APIBase
		if base == "" && o.BaseURL != "" {
			base = strings.TrimRight(o.BaseURL, "/") + "/api/v4"
		}
		if base == "" {
			base = "https://" + host + "/api/v4"
		}
		url, header, value = strings.TrimRight(base, "/")+"/user", "PRIVATE-TOKEN", token
	default:
		st.Status = AuthUnsupported
		return st, fmt.Errorf("%w: no token check for provider %q", ErrUnsupported, platform)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return st, err
	}
	req.Header.Set(header, value)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return st, fmt.Errorf("cannot reach %s: %s", host, scrubSecret(friendlyNetError(err, host), token))
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		st.Status = AuthLoggedIn
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		st.Status, st.Detail = AuthNotLoggedIn, "the token was rejected by "+host
	default:
		return st, fmt.Errorf("%s answered HTTP %d", host, resp.StatusCode)
	}
	return st, nil
}

// noRunner keeps the project-less check from reading the origin of whatever
// directory the process happens to run in.
type noRunner struct{}

func (noRunner) Run(context.Context, string, string, ...string) (string, error) {
	return "", fmt.Errorf("no project")
}
