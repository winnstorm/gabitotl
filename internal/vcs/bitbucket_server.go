package vcs

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/vcshosts"
)

// ServerRepo names a Bitbucket Server / Data Center repository: the project key
// (or "~user" for a personal repository) and the repository slug.
type ServerRepo struct{ Project, Slug string }

// ParseServerRemote reads project and repository from an origin URL of a Bitbucket
// Data Center server: https://host[:port][/context]/scm/PROJ/repo.git,
// ssh://git@host:7999/proj/repo.git or git@host:proj/repo.git. The API base URL is
// never derived from it (an SSH port is not the HTTP port): it comes from the registry.
func ParseServerRemote(remote string) (ServerRepo, error) {
	remote = strings.TrimSpace(remote)
	var path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return ServerRepo{}, errors.New("vcs: cannot read the origin URL")
		}
		path = u.Path
	} else if _, after, ok := strings.Cut(remote, ":"); ok { // scp-like: git@host:proj/repo.git
		path = after
	}
	parts := strings.Split(strings.TrimSuffix(strings.Trim(path, "/"), ".git"), "/")
	if n := len(parts); n >= 3 && parts[n-3] == "scm" {
		parts = parts[n-2:]
	}
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ServerRepo{}, errors.New("vcs: cannot find project/repository in the origin URL")
	}
	return ServerRepo{Project: parts[0], Slug: parts[1]}, nil
}

func (r ServerRepo) api() string {
	return "/projects/" + url.PathEscape(r.Project) + "/repos/" + url.PathEscape(r.Slug)
}

func (r ServerRepo) ref(branch string) map[string]any {
	return map[string]any{
		"id":         "refs/heads/" + branch,
		"repository": map[string]any{"slug": r.Slug, "project": map[string]string{"key": r.Project}},
	}
}

// BitbucketServer drives Bitbucket Server / Data Center through REST API 1.0
// (/rest/api/latest, /rest/build-status/latest and /rest/branch-utils/latest). The
// token is an HTTP access token sent as a bearer. The server's web URL (scheme,
// port, context path) comes from the registry.
type BitbucketServer struct {
	Runner  Runner
	HTTP    *http.Client
	Servers vcshosts.Registry
	// Token returns the token of a host (without port); "" means none.
	Token func(host string) (string, error)
}

// Name implements PRProvider.
func (BitbucketServer) Name() string { return "bitbucket-server" }

// noRedirect returns a copy of c that never follows a redirect, so the token
// header cannot be forwarded to another host.
func noRedirect(c *http.Client) *http.Client {
	cp := *c
	cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &cp
}

func (b BitbucketServer) client() *http.Client {
	c := b.HTTP
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	return noRedirect(c)
}

// friendlyNetError words a transport failure without the URL or any secret; a
// certificate problem is named so a corporate CA issue is recognisable.
func friendlyNetError(err error, host string) string {
	var ua x509.UnknownAuthorityError
	var ci x509.CertificateInvalidError
	var he x509.HostnameError
	var cv *tls.CertificateVerificationError
	switch {
	case errors.As(err, &ua):
		return "certificate not trusted by this system (is the corporate CA installed in the OS trust store?)"
	case errors.As(err, &he):
		return "certificate does not match the host name " + host
	case errors.As(err, &ci):
		return "certificate is not valid (expired or not usable)"
	case errors.As(err, &cv):
		return "certificate could not be verified by this system"
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return err.Error()
}

// bsTarget is everything one call needs.
type bsTarget struct {
	base  string
	host  string
	repo  ServerRepo
	token string
}

func (b BitbucketServer) target(ctx context.Context, dir string) (bsTarget, error) {
	remote, err := runnerOr(b.Runner).Run(ctx, dir, "git", "remote", "get-url", "origin")
	if err != nil || remote == "" {
		return bsTarget{}, ErrNoRemote
	}
	repo, err := ParseServerRemote(remote)
	if err != nil {
		return bsTarget{}, err
	}
	host := remoteHost(remote)
	e, ok := vcshosts.Lookup(b.Servers, host)
	if !ok || e.Platform != vcshosts.BitbucketServer {
		return bsTarget{}, fmt.Errorf("%w: %s is not a registered Bitbucket server; run: gabitotl vcs login --url https://%s", ErrUnsupported, host, host)
	}
	tok := ""
	if b.Token != nil {
		if tok, err = b.Token(host); err != nil {
			return bsTarget{}, err
		}
	}
	if strings.TrimSpace(tok) == "" {
		return bsTarget{}, fmt.Errorf("%w: no Bitbucket token for %s", ErrNotLoggedIn, host)
	}
	return bsTarget{base: e.URL, host: host, repo: repo, token: strings.TrimSpace(tok)}, nil
}

// do sends one API request. Errors name the method, path and status; never the
// token, a header or the request body.
func (b BitbucketServer) do(ctx context.Context, t bsTarget, method, path string, query url.Values, in, out any) error {
	u := t.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := b.client().Do(req)
	if err != nil {
		return scrubError(fmt.Errorf("bitbucket-server: %s %s: %s", method, path, friendlyNetError(err, t.host)), t.token)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		err := fmt.Errorf("bitbucket-server: %s %s: HTTP %d%s", method, path, resp.StatusCode, serverMessage(raw))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("%w: %w", ErrNotLoggedIn, scrubError(err, t.token))
		}
		return scrubError(err, t.token)
	}
	if out != nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("bitbucket-server: %s %s: unreadable response", method, path)
		}
	}
	return nil
}

// serverMessage extracts errors[0].message from a Data Center error body, shortened.
func serverMessage(raw []byte) string {
	var e struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(raw, &e) != nil || len(e.Errors) == 0 || e.Errors[0].Message == "" {
		return ""
	}
	m := e.Errors[0].Message
	if len(m) > 200 {
		m = m[:200]
	}
	return ": " + m
}

type bsPR struct {
	ID      int    `json:"id"`
	Version int    `json:"version"`
	Title   string `json:"title"`
	State   string `json:"state"`
	FromRef struct {
		ID           string `json:"id"`
		LatestCommit string `json:"latestCommit"`
	} `json:"fromRef"`
	Links struct {
		Self []struct {
			Href string `json:"href"`
		} `json:"self"`
	} `json:"links"`
}

func (p bsPR) pr() PR {
	out := PR{ID: fmt.Sprint(p.ID), Number: p.ID}
	if len(p.Links.Self) > 0 {
		out.URL = p.Links.Self[0].Href
	}
	return out
}

const bsAPI = "/rest/api/latest"

func (b BitbucketServer) headBranch(ctx context.Context, dir, head string) (string, error) {
	if head != "" {
		return head, nil
	}
	cur, err := runnerOr(b.Runner).Run(ctx, dir, "git", "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil || cur == "" {
		return "", errors.New("vcs: cannot tell the current branch; pass the head branch")
	}
	return cur, nil
}

func (b BitbucketServer) findOpen(ctx context.Context, t bsTarget, head string) (PR, bool, error) {
	var found struct {
		Values []bsPR `json:"values"`
	}
	q := url.Values{"state": {"OPEN"}, "direction": {"OUTGOING"}, "at": {"refs/heads/" + head}, "limit": {"25"}}
	if err := b.do(ctx, t, http.MethodGet, bsAPI+t.repo.api()+"/pull-requests", q, nil, &found); err != nil {
		return PR{}, false, err
	}
	for _, p := range found.Values {
		if p.FromRef.ID == "refs/heads/"+head && p.State == "OPEN" {
			return p.pr(), true, nil
		}
	}
	return PR{}, false, nil
}

// FindPR implements PRFinder.
func (b BitbucketServer) FindPR(ctx context.Context, dir, head string) (PR, bool, error) {
	t, err := b.target(ctx, dir)
	if err != nil {
		return PR{}, false, err
	}
	if head, err = b.headBranch(ctx, dir, head); err != nil {
		return PR{}, false, err
	}
	return b.findOpen(ctx, t, head)
}

// CreatePR implements PRProvider. It is idempotent: an open pull request of the
// source branch is returned instead of opening a second one. Draft is sent for
// servers that support it (8.18+); older servers ignore the field.
func (b BitbucketServer) CreatePR(ctx context.Context, dir string, req PRRequest) (PR, error) {
	if err := checkRequest(req); err != nil {
		return PR{}, err
	}
	t, err := b.target(ctx, dir)
	if err != nil {
		return PR{}, err
	}
	head, err := b.headBranch(ctx, dir, req.Head)
	if err != nil {
		return PR{}, err
	}
	if pr, ok, err := b.findOpen(ctx, t, head); err != nil || ok {
		return pr, err
	}
	base := req.Base
	if base == "" {
		var def struct {
			DisplayID string `json:"displayId"`
		}
		if err := b.do(ctx, t, http.MethodGet, bsAPI+t.repo.api()+"/branches/default", nil, nil, &def); err != nil {
			return PR{}, err
		}
		base = def.DisplayID
	}
	body := map[string]any{
		"title": req.Title, "description": req.Body,
		"fromRef": t.repo.ref(head), "toRef": t.repo.ref(base),
	}
	if req.Draft {
		body["draft"] = true
	}
	var created bsPR
	if err := b.do(ctx, t, http.MethodPost, bsAPI+t.repo.api()+"/pull-requests", nil, body, &created); err != nil {
		return PR{}, err
	}
	return created.pr(), nil
}

func (b BitbucketServer) getPR(ctx context.Context, t bsTarget, pr PR) (bsPR, error) {
	id, err := prID(pr)
	if err != nil {
		return bsPR{}, err
	}
	var cur bsPR
	err = b.do(ctx, t, http.MethodGet, bsAPI+t.repo.api()+"/pull-requests/"+id, nil, nil, &cur)
	return cur, err
}

// bsStrategy maps a merge method to a Data Center merge strategy id.
func bsStrategy(method string) string {
	switch method {
	case "squash":
		return "squash"
	case "rebase":
		return "rebase-no-ff"
	}
	return "no-ff"
}

// MergePR implements Host: it reads the current version of the pull request
// (the server refuses a stale one with 409), merges, and then deletes the source
// branch through the branch-utils API when asked.
func (b BitbucketServer) MergePR(ctx context.Context, dir string, pr PR, opts MergeOptions) error {
	if err := checkMergeMethod(opts.Method); err != nil {
		return err
	}
	t, err := b.target(ctx, dir)
	if err != nil {
		return err
	}
	cur, err := b.getPR(ctx, t, pr)
	if err != nil {
		return err
	}
	path := bsAPI + t.repo.api() + "/pull-requests/" + fmt.Sprint(cur.ID) + "/merge"
	body := map[string]any{"version": cur.Version, "strategyId": bsStrategy(opts.Method)}
	if err := b.do(ctx, t, http.MethodPost, path, url.Values{"version": {fmt.Sprint(cur.Version)}}, body, nil); err != nil {
		return err
	}
	if opts.DeleteBranch && cur.FromRef.ID != "" {
		del := map[string]any{"name": cur.FromRef.ID, "dryRun": false}
		if err := b.do(ctx, t, http.MethodDelete, "/rest/branch-utils/latest"+t.repo.api()+"/branches", nil, del, nil); err != nil {
			return fmt.Errorf("the pull request was merged, but its source branch could not be deleted: %w", err)
		}
	}
	return nil
}

// SetBase implements Host.
func (b BitbucketServer) SetBase(ctx context.Context, dir string, pr PR, newBase string) error {
	t, err := b.target(ctx, dir)
	if err != nil {
		return err
	}
	cur, err := b.getPR(ctx, t, pr)
	if err != nil {
		return err
	}
	body := map[string]any{"version": cur.Version, "title": cur.Title, "toRef": t.repo.ref(newBase)}
	return b.do(ctx, t, http.MethodPut, bsAPI+t.repo.api()+"/pull-requests/"+fmt.Sprint(cur.ID), nil, body, nil)
}

// ChecksState implements Host from the build statuses of the head commit.
func (b BitbucketServer) ChecksState(ctx context.Context, dir string, pr PR) (Checks, error) {
	t, err := b.target(ctx, dir)
	if err != nil {
		return Checks{}, err
	}
	cur, err := b.getPR(ctx, t, pr)
	if err != nil {
		return Checks{}, err
	}
	if cur.FromRef.LatestCommit == "" {
		return Checks{State: ChecksNone}, nil
	}
	var res struct {
		Values []struct {
			Key   string `json:"key"`
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"values"`
	}
	path := "/rest/build-status/latest/commits/" + url.PathEscape(cur.FromRef.LatestCommit)
	if err := b.do(ctx, t, http.MethodGet, path, url.Values{"limit": {"100"}}, nil, &res); err != nil {
		return Checks{}, err
	}
	var failing, pending []string
	for _, s := range res.Values {
		name := s.Name
		if name == "" {
			name = s.Key
		}
		switch strings.ToUpper(s.State) {
		case "FAILED":
			failing = append(failing, name)
		case "INPROGRESS":
			pending = append(pending, name)
		}
	}
	return combineChecks(len(res.Values), failing, pending), nil
}

// AuthStatus implements Host: it asks the registered server whether the stored
// token is accepted.
func (b BitbucketServer) AuthStatus(ctx context.Context, dir string) (AuthState, error) {
	st := AuthState{Provider: "bitbucket-server", Command: "gabitotl vcs login"}
	t, err := b.target(ctx, dir)
	switch {
	case errors.Is(err, ErrUnsupported):
		st.Status, st.Detail, st.Command = AuthUnsupported, err.Error(), ""
		st.Fix = "Register the server first: gabitotl vcs login --url https://your-bitbucket-host"
		return st, nil
	case errors.Is(err, ErrNotLoggedIn):
		st.Status, st.Detail = AuthNotLoggedIn, err.Error()
		st.Fix = "Create an HTTP access token in Bitbucket and run: gabitotl vcs login"
		return st, nil
	case err != nil:
		return AuthState{}, err
	}
	user, ok, err := checkBitbucketServer(ctx, b.client(), t.base, t.host, t.token)
	if err != nil {
		return AuthState{}, err
	}
	if !ok {
		st.Status, st.Detail = AuthNotLoggedIn, "the token was rejected by "+t.host
		st.Fix = "Create a new HTTP access token in Bitbucket and run: gabitotl vcs login"
		return st, nil
	}
	st.Status, st.Command, st.Detail = AuthLoggedIn, "", "signed in as "+user
	return st, nil
}

// checkBitbucketServer asks an endpoint that needs authentication who the token
// belongs to. A server that lets the request through anonymously (no
// X-AUSERNAME, or "anonymous") does not prove the token: that is "not accepted".
func checkBitbucketServer(ctx context.Context, c *http.Client, base, host, token string) (user string, ok bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+bsAPI+"/profile/recent/repos?limit=1", nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := noRedirect(c).Do(req)
	if err != nil {
		return "", false, scrubError(fmt.Errorf("cannot reach %s: %s", host, friendlyNetError(err, host)), token)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", false, nil
	case resp.StatusCode == http.StatusOK:
		u := resp.Header.Get("X-AUSERNAME")
		if u == "" || strings.EqualFold(u, "anonymous") {
			return "", false, nil
		}
		return u, true, nil
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return "", false, fmt.Errorf("%s redirected the request (HTTP %d); use the final URL of the server", host, resp.StatusCode)
	case resp.StatusCode == http.StatusNotFound:
		return "", false, fmt.Errorf("%s does not look like a Bitbucket server at that URL (HTTP 404); check the URL and context path", host)
	}
	return "", false, fmt.Errorf("%s answered HTTP %d", host, resp.StatusCode)
}
