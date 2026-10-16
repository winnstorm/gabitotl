package vcs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBitbucketAPI is the Bitbucket Cloud REST API root.
const DefaultBitbucketAPI = "https://api.bitbucket.org"

// DefaultBitbucketTokenEnv is the variable `vcs login` suggests for the token.
const DefaultBitbucketTokenEnv = "BITBUCKET_TOKEN"

// Credential is what authenticates a Bitbucket Cloud request: a bearer token
// (workspace, repository or personal access token) or, when User is set, an app
// password sent as HTTP Basic. It must never be logged or printed.
type Credential struct {
	User   string
	Secret string
}

// ParseCredential reads the value behind a token reference: a plain token is a
// bearer token; user:app-password is an app password (Basic).
func ParseCredential(value string) Credential {
	value = strings.TrimSpace(value)
	if u, s, ok := strings.Cut(value, ":"); ok && u != "" && s != "" {
		return Credential{User: u, Secret: s}
	}
	return Credential{Secret: value}
}

// TokenEnvName returns the variable a per-person auth reference points at:
// env:NAME gives NAME and an empty or "native" reference gives the default.
// keychain: references are not supported for Bitbucket.
func TokenEnvName(ref string) (string, error) {
	ref = strings.Trim(strings.TrimSpace(ref), `"'`)
	switch {
	case ref == "" || ref == "native":
		return DefaultBitbucketTokenEnv, nil
	case strings.HasPrefix(ref, "env:") && len(ref) > 4:
		return ref[4:], nil
	}
	return "", fmt.Errorf("%w: Bitbucket reads its token from an environment variable (auth: env:NAME), not from %q", ErrUnsupported, ref)
}

// ResolveCredential looks the token behind ref up with lookup (the process
// environment, then the 0600 secrets file). A missing value is ErrNotLoggedIn.
func ResolveCredential(ref string, lookup func(string) string) (Credential, error) {
	name, err := TokenEnvName(ref)
	if err != nil {
		return Credential{}, err
	}
	if lookup == nil {
		return Credential{}, fmt.Errorf("%w: no token source", ErrNotLoggedIn)
	}
	v := strings.TrimSpace(lookup(name))
	if v == "" {
		return Credential{}, fmt.Errorf("%w: %s is empty or not set", ErrNotLoggedIn, name)
	}
	return ParseCredential(v), nil
}

// BitbucketRepo names a Bitbucket Cloud repository.
type BitbucketRepo struct{ Workspace, Repo string }

// ParseBitbucketRemote reads workspace and repository from an origin URL (https
// or ssh). Any host other than bitbucket.org (Bitbucket Server and Data Center)
// is ErrUnsupported.
func ParseBitbucketRemote(remote string) (BitbucketRepo, error) {
	remote = strings.TrimSpace(remote)
	host := remoteHost(remote)
	if host != "bitbucket.org" && !strings.HasSuffix(host, ".bitbucket.org") {
		return BitbucketRepo{}, fmt.Errorf("%w: only Bitbucket Cloud (bitbucket.org) is supported; Bitbucket Server and Data Center are not", ErrUnsupported)
	}
	var path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return BitbucketRepo{}, fmt.Errorf("vcs: cannot read the origin URL: %w", err)
		}
		path = u.Path
	} else if _, after, ok := strings.Cut(remote, ":"); ok { // scp-like: git@bitbucket.org:ws/repo.git
		path = after
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return BitbucketRepo{}, fmt.Errorf("vcs: cannot find workspace/repository in the origin URL")
	}
	return BitbucketRepo{Workspace: parts[0], Repo: parts[1]}, nil
}

// Bitbucket drives Bitbucket Cloud through its REST API 2.0. Bitbucket has no
// official CLI, so authentication is a token read from a reference.
type Bitbucket struct {
	Runner  Runner // git remote get-url origin / current branch
	HTTP    *http.Client
	BaseURL string
	Creds   func() (Credential, error)
}

// Name implements PRProvider.
func (Bitbucket) Name() string { return "bitbucket" }

func (b Bitbucket) client() *http.Client {
	if b.HTTP != nil {
		return b.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (b Bitbucket) credential() (Credential, error) {
	if b.Creds == nil {
		return Credential{}, fmt.Errorf("%w: no Bitbucket token is configured", ErrNotLoggedIn)
	}
	return b.Creds()
}

func (b Bitbucket) repo(ctx context.Context, dir string) (BitbucketRepo, error) {
	remote, err := runnerOr(b.Runner).Run(ctx, dir, "git", "remote", "get-url", "origin")
	if err != nil || remote == "" {
		return BitbucketRepo{}, ErrNoRemote
	}
	return ParseBitbucketRemote(remote)
}

func (r BitbucketRepo) prs() string {
	return "/2.0/repositories/" + url.PathEscape(r.Workspace) + "/" + url.PathEscape(r.Repo) + "/pullrequests"
}

// do sends one API request. The error names the method, path and status, never a
// header, a token or the request body.
func (b Bitbucket) do(ctx context.Context, cred Credential, method, path string, query url.Values, in, out any) error {
	base := strings.TrimRight(b.BaseURL, "/")
	if base == "" {
		base = DefaultBitbucketAPI
	}
	u := base + path
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
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cred.User != "" {
		req.SetBasicAuth(cred.User, cred.Secret)
	} else {
		req.Header.Set("Authorization", "Bearer "+cred.Secret)
	}
	resp, err := b.client().Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // drop the URL wrapper: the message stays free of anything but the cause
		}
		return fmt.Errorf("bitbucket: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		err := fmt.Errorf("bitbucket: %s %s: HTTP %d%s", method, path, resp.StatusCode, apiMessage(raw))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("%w: %w", ErrNotLoggedIn, err)
		}
		return err
	}
	if out != nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("bitbucket: %s %s: unreadable response", method, path)
		}
	}
	return nil
}

// apiMessage extracts error.message from a Bitbucket error body, shortened.
func apiMessage(raw []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Error.Message == "" {
		return ""
	}
	m := e.Error.Message
	if len(m) > 200 {
		m = m[:200]
	}
	return ": " + m
}

type bbPR struct {
	ID    int `json:"id"`
	Links struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}

func (p bbPR) pr() PR {
	return PR{URL: p.Links.HTML.Href, ID: fmt.Sprint(p.ID), Number: p.ID}
}

// CreatePR implements PRProvider. It is idempotent: an open pull request of the
// source branch is returned instead of opening a second one.
func (b Bitbucket) CreatePR(ctx context.Context, dir string, req PRRequest) (PR, error) {
	if err := checkRequest(req); err != nil {
		return PR{}, err
	}
	cred, err := b.credential()
	if err != nil {
		return PR{}, err
	}
	repo, err := b.repo(ctx, dir)
	if err != nil {
		return PR{}, err
	}
	head, err := b.headBranch(ctx, dir, req.Head)
	if err != nil {
		return PR{}, err
	}
	if pr, ok, err := b.findOpen(ctx, cred, repo, head); err != nil || ok {
		return pr, err
	}
	body := map[string]any{
		"title":       req.Title,
		"description": req.Body,
		"source":      map[string]any{"branch": map[string]string{"name": head}},
	}
	if req.Base != "" {
		body["destination"] = map[string]any{"branch": map[string]string{"name": req.Base}}
	}
	if req.Draft {
		body["draft"] = true
	}
	var created bbPR
	if err := b.do(ctx, cred, http.MethodPost, repo.prs(), nil, body, &created); err != nil {
		return PR{}, err
	}
	return created.pr(), nil
}

// bbStrategy maps a merge method to Bitbucket's merge_strategy.
func bbStrategy(method string) string {
	switch method {
	case "squash":
		return "squash"
	case "rebase":
		return "fast_forward"
	}
	return "merge_commit"
}

// MergePR implements Host.
func (b Bitbucket) MergePR(ctx context.Context, dir string, pr PR, opts MergeOptions) error {
	if err := checkMergeMethod(opts.Method); err != nil {
		return err
	}
	id, err := prID(pr)
	if err != nil {
		return err
	}
	cred, repo, err := b.target(ctx, dir)
	if err != nil {
		return err
	}
	body := map[string]any{"merge_strategy": bbStrategy(opts.Method), "close_source_branch": opts.DeleteBranch}
	return b.do(ctx, cred, http.MethodPost, repo.prs()+"/"+id+"/merge", nil, body, nil)
}

// SetBase implements Host.
func (b Bitbucket) SetBase(ctx context.Context, dir string, pr PR, newBase string) error {
	id, err := prID(pr)
	if err != nil {
		return err
	}
	cred, repo, err := b.target(ctx, dir)
	if err != nil {
		return err
	}
	body := map[string]any{"destination": map[string]any{"branch": map[string]string{"name": newBase}}}
	return b.do(ctx, cred, http.MethodPut, repo.prs()+"/"+id, nil, body, nil)
}

// ChecksState implements Host from the commit statuses of the pull request.
func (b Bitbucket) ChecksState(ctx context.Context, dir string, pr PR) (Checks, error) {
	id, err := prID(pr)
	if err != nil {
		return Checks{}, err
	}
	cred, repo, err := b.target(ctx, dir)
	if err != nil {
		return Checks{}, err
	}
	var res struct {
		Values []struct {
			Name  string `json:"name"`
			Key   string `json:"key"`
			State string `json:"state"`
		} `json:"values"`
	}
	if err := b.do(ctx, cred, http.MethodGet, repo.prs()+"/"+id+"/statuses", url.Values{"pagelen": {"100"}}, nil, &res); err != nil {
		return Checks{}, err
	}
	var failing, pending []string
	for _, s := range res.Values {
		name := s.Name
		if name == "" {
			name = s.Key
		}
		switch strings.ToUpper(s.State) {
		case "FAILED", "STOPPED":
			failing = append(failing, name)
		case "INPROGRESS":
			pending = append(pending, name)
		}
	}
	return combineChecks(len(res.Values), failing, pending), nil
}

func (b Bitbucket) target(ctx context.Context, dir string) (Credential, BitbucketRepo, error) {
	cred, err := b.credential()
	if err != nil {
		return Credential{}, BitbucketRepo{}, err
	}
	repo, err := b.repo(ctx, dir)
	return cred, repo, err
}

// AuthStatus implements Host: it checks the origin is Bitbucket Cloud, then asks
// GET /2.0/user whether the token works.
func (b Bitbucket) AuthStatus(ctx context.Context, dir string) (AuthState, error) {
	st := AuthState{Provider: "bitbucket", Command: "gabitotl vcs login"}
	if remote, err := runnerOr(b.Runner).Run(ctx, dir, "git", "remote", "get-url", "origin"); err == nil && remote != "" {
		if _, err := ParseBitbucketRemote(remote); errors.Is(err, ErrUnsupported) {
			st.Status, st.Command, st.Detail = AuthUnsupported, "", err.Error()
			st.Fix = "Bitbucket Server and Data Center are not supported; only Bitbucket Cloud (bitbucket.org) is."
			return st, nil
		}
	}
	notLoggedIn := func(detail string) AuthState {
		st.Status, st.Detail = AuthNotLoggedIn, detail
		st.Fix = "Create a Bitbucket token, export it in an environment variable and run: gabitotl vcs login"
		return st
	}
	cred, err := b.credential()
	if err != nil {
		if errors.Is(err, ErrNotLoggedIn) {
			return notLoggedIn(err.Error()), nil
		}
		return AuthState{}, err
	}
	if err := b.do(ctx, cred, http.MethodGet, "/2.0/user", nil, nil, nil); err != nil {
		if errors.Is(err, ErrNotLoggedIn) {
			return notLoggedIn("the token was rejected by Bitbucket"), nil
		}
		return AuthState{}, err
	}
	st.Status, st.Command = AuthLoggedIn, ""
	return st, nil
}

// headBranch is head, or the current branch when head is empty.
func (b Bitbucket) headBranch(ctx context.Context, dir, head string) (string, error) {
	if head != "" {
		return head, nil
	}
	cur, err := runnerOr(b.Runner).Run(ctx, dir, "git", "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil || cur == "" {
		return "", errors.New("vcs: cannot tell the current branch; pass the head branch")
	}
	return cur, nil
}

// findOpen returns the open pull request whose source branch is head.
func (b Bitbucket) findOpen(ctx context.Context, cred Credential, repo BitbucketRepo, head string) (PR, bool, error) {
	var found struct {
		Values []bbPR `json:"values"`
	}
	q := url.Values{"q": {fmt.Sprintf(`source.branch.name = %q AND state = "OPEN"`, head)}, "pagelen": {"1"}}
	if err := b.do(ctx, cred, http.MethodGet, repo.prs(), q, nil, &found); err != nil {
		return PR{}, false, err
	}
	if len(found.Values) == 0 {
		return PR{}, false, nil
	}
	return found.Values[0].pr(), true, nil
}

// FindPR implements PRFinder.
func (b Bitbucket) FindPR(ctx context.Context, dir, head string) (PR, bool, error) {
	cred, err := b.credential()
	if err != nil {
		return PR{}, false, err
	}
	repo, err := b.repo(ctx, dir)
	if err != nil {
		return PR{}, false, err
	}
	if head, err = b.headBranch(ctx, dir, head); err != nil {
		return PR{}, false, err
	}
	return b.findOpen(ctx, cred, repo, head)
}
