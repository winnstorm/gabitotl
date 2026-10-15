package vcs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const bbToken = "s3cr3t-token-value"

// bbServer records requests and answers from a handler.
type bbServer struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []bbReq
}

type bbReq struct {
	Method, Path, Query, Auth, Body string
}

func newBB(t *testing.T, handle func(r bbReq, w http.ResponseWriter)) *bbServer {
	t.Helper()
	s := &bbServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		req := bbReq{r.Method, r.URL.Path, r.URL.Query().Get("q"), r.Header.Get("Authorization"), string(b)}
		s.mu.Lock()
		s.reqs = append(s.reqs, req)
		s.mu.Unlock()
		handle(req, w)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *bbServer) host(remote string, cred Credential) Bitbucket {
	run := &scripted{handle: func(name string, args []string) (string, error) {
		if name == "git" && args[0] == "symbolic-ref" {
			return "feature/x", nil
		}
		return remote, nil
	}}
	return Bitbucket{Runner: run, BaseURL: s.URL, Creds: func() (Credential, error) { return cred, nil }}
}

const bbRemote = "git@bitbucket.org:acme/app.git"

func TestParseBitbucketRemote(t *testing.T) {
	for _, in := range []string{
		"https://bitbucket.org/acme/app.git", "https://user@bitbucket.org/acme/app.git", "https://bitbucket.org/acme/app",
		"git@bitbucket.org:acme/app.git", "ssh://git@bitbucket.org/acme/app.git",
	} {
		r, err := ParseBitbucketRemote(in)
		if err != nil || r.Workspace != "acme" || r.Repo != "app" {
			t.Errorf("%q: %+v %v", in, r, err)
		}
	}
	for _, in := range []string{"https://bitbucket.acme.io/scm/proj/app.git", "ssh://git@bitbucket.acme.io:7999/proj/app.git", "git@github.com:acme/app.git"} {
		if _, err := ParseBitbucketRemote(in); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%q must be unsupported (Server / Data Center): %v", in, err)
		}
	}
	for _, in := range []string{"https://bitbucket.org/acme", "git@bitbucket.org:acme/app/extra.git", "https://bitbucket.org/"} {
		if _, err := ParseBitbucketRemote(in); err == nil || errors.Is(err, ErrUnsupported) {
			t.Errorf("%q must be a parse error: %v", in, err)
		}
	}
}

func TestCredentialResolution(t *testing.T) {
	lookup := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if c, err := ResolveCredential("env:MY_BB", lookup(map[string]string{"MY_BB": " tok "})); err != nil || c != (Credential{Secret: "tok"}) {
		t.Fatalf("%+v %v", c, err)
	}
	if c, err := ResolveCredential("env:MY_BB", lookup(map[string]string{"MY_BB": "ana:apppass"})); err != nil || c != (Credential{User: "ana", Secret: "apppass"}) {
		t.Fatalf("an app password is user:secret: %+v %v", c, err)
	}
	if c, err := ResolveCredential("", lookup(map[string]string{"BITBUCKET_TOKEN": "t"})); err != nil || c.Secret != "t" {
		t.Fatalf("the default variable: %+v %v", c, err)
	}
	if _, err := ResolveCredential("env:MISSING", lookup(nil)); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("%v", err)
	}
	if _, err := ResolveCredential("keychain:x", lookup(nil)); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("%v", err)
	}
}

func TestBitbucketCreateReturnsAnExistingPR(t *testing.T) {
	s := newBB(t, func(r bbReq, w http.ResponseWriter) {
		_, _ = io.WriteString(w, `{"values":[{"id":4,"links":{"html":{"href":"https://bitbucket.org/acme/app/pull-requests/4"}}}]}`)
	})
	pr, err := s.host(bbRemote, Credential{Secret: bbToken}).CreatePR(context.Background(), "", PRRequest{Title: "feat: x", Head: "feature/x"})
	if err != nil || pr.Number != 4 || pr.ID != "4" || pr.URL != "https://bitbucket.org/acme/app/pull-requests/4" {
		t.Fatalf("%+v %v", pr, err)
	}
	if len(s.reqs) != 1 || s.reqs[0].Method != "GET" || s.reqs[0].Path != "/2.0/repositories/acme/app/pullrequests" {
		t.Fatalf("%+v", s.reqs)
	}
	has(t, s.reqs[0].Query, `source.branch.name = "feature/x"`, `state = "OPEN"`)
	if s.reqs[0].Auth != "Bearer "+bbToken {
		t.Fatalf("auth header = %q", s.reqs[0].Auth)
	}
}

func TestBitbucketCreateOpensAPR(t *testing.T) {
	s := newBB(t, func(r bbReq, w http.ResponseWriter) {
		if r.Method == "GET" {
			_, _ = io.WriteString(w, `{"values":[]}`)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":11,"links":{"html":{"href":"https://bitbucket.org/acme/app/pull-requests/11"}}}`)
	})
	h := s.host(bbRemote, Credential{User: "ana", Secret: "apppass"})
	pr, err := h.CreatePR(context.Background(), "", PRRequest{Title: "feat: x", Body: "b", Base: "main", Draft: true}) // head from the current branch
	if err != nil || pr.Number != 11 {
		t.Fatalf("%+v %v", pr, err)
	}
	post := s.reqs[1]
	if post.Method != "POST" || post.Path != "/2.0/repositories/acme/app/pullrequests" {
		t.Fatalf("%+v", post)
	}
	if want := "Basic YW5hOmFwcHBhc3M="; post.Auth != want {
		t.Fatalf("an app password is sent as Basic: %q", post.Auth)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(post.Body), &body); err != nil {
		t.Fatal(err)
	}
	src := body["source"].(map[string]any)["branch"].(map[string]any)["name"]
	dst := body["destination"].(map[string]any)["branch"].(map[string]any)["name"]
	if body["title"] != "feat: x" || body["description"] != "b" || src != "feature/x" || dst != "main" || body["draft"] != true {
		t.Fatalf("%v", body)
	}
	if _, err := h.CreatePR(context.Background(), "", PRRequest{Title: "x", Body: "Co-Authored-By: A <a@b.c>"}); !errors.Is(err, ErrAttribution) {
		t.Fatalf("%v", err)
	}
}

func TestBitbucketMergeStrategiesRetargetAndChecks(t *testing.T) {
	statuses := `{"values":[{"name":"build","state":"SUCCESSFUL"}]}`
	s := newBB(t, func(r bbReq, w http.ResponseWriter) {
		if strings.HasSuffix(r.Path, "/statuses") {
			_, _ = io.WriteString(w, statuses)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	})
	h := s.host(bbRemote, Credential{Secret: bbToken})
	pr := PR{ID: "4", Number: 4}
	for method, strategy := range map[string]string{"squash": "squash", "merge": "merge_commit", "rebase": "fast_forward"} {
		s.reqs = nil
		if err := h.MergePR(context.Background(), "", pr, MergeOptions{Method: method, DeleteBranch: method == "squash"}); err != nil {
			t.Fatal(err)
		}
		r := s.reqs[0]
		var b map[string]any
		_ = json.Unmarshal([]byte(r.Body), &b)
		if r.Method != "POST" || r.Path != "/2.0/repositories/acme/app/pullrequests/4/merge" || b["merge_strategy"] != strategy || b["close_source_branch"] != (method == "squash") {
			t.Errorf("%s: %+v", method, r)
		}
	}
	if err := h.MergePR(context.Background(), "", pr, MergeOptions{Method: "x"}); err == nil {
		t.Fatal("unknown method")
	}
	s.reqs = nil
	if err := h.SetBase(context.Background(), "", pr, "main"); err != nil {
		t.Fatal(err)
	}
	if r := s.reqs[0]; r.Method != "PUT" || r.Path != "/2.0/repositories/acme/app/pullrequests/4" || !strings.Contains(r.Body, `"name":"main"`) || !strings.Contains(r.Body, "destination") {
		t.Fatalf("%+v", r)
	}
	for body, want := range map[string]string{
		`{"values":[{"name":"a","state":"SUCCESSFUL"}]}`:                                   ChecksPassing,
		`{"values":[{"name":"a","state":"SUCCESSFUL"},{"name":"b","state":"FAILED"}]}`:     ChecksFailing,
		`{"values":[{"name":"a","state":"STOPPED"}]}`:                                      ChecksFailing,
		`{"values":[{"name":"a","state":"SUCCESSFUL"},{"name":"b","state":"INPROGRESS"}]}`: ChecksPending,
		`{"values":[]}`: ChecksNone,
	} {
		statuses = body
		got, err := h.ChecksState(context.Background(), "", pr)
		if err != nil || got.State != want {
			t.Errorf("%s: %+v %v", body, got, err)
		}
	}
}

func TestBitbucketAuthStatesAndTheTokenNeverLeaks(t *testing.T) {
	s := newBB(t, func(r bbReq, w http.ResponseWriter) {
		if r.Auth != "Bearer "+bbToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"Bad credentials"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"username":"ana"}`)
	})
	ctx := context.Background()
	st, err := s.host(bbRemote, Credential{Secret: bbToken}).AuthStatus(ctx, "")
	if err != nil || !st.OK() || s.reqs[0].Path != "/2.0/user" {
		t.Fatalf("%+v %v %+v", st, err, s.reqs)
	}
	st, err = s.host(bbRemote, Credential{Secret: "wrong-" + bbToken}).AuthStatus(ctx, "")
	if err != nil || st.Status != AuthNotLoggedIn || st.Command != "gabitotl vcs login" {
		t.Fatalf("rejected token: %+v %v", st, err)
	}
	has(t, st.Fix, "gabitotl vcs login")
	// No credential at all.
	h := s.host(bbRemote, Credential{})
	h.Creds = nil
	if st, _ := h.AuthStatus(ctx, ""); st.Status != AuthNotLoggedIn {
		t.Fatalf("%+v", st)
	}
	// Bitbucket Server / Data Center.
	if st, _ := s.host("ssh://git@bitbucket.acme.io:7999/p/app.git", Credential{Secret: bbToken}).AuthStatus(ctx, ""); st.Status != AuthUnsupported || !strings.Contains(st.Fix, "Data Center") {
		t.Fatalf("%+v", st)
	}
	// An API failure carries the status and message but never the token.
	_, err = s.host(bbRemote, Credential{Secret: "wrong-" + bbToken}).ChecksState(ctx, "", PR{ID: "4"})
	if err == nil || !errors.Is(err, ErrNotLoggedIn) || strings.Contains(err.Error(), bbToken) || !strings.Contains(err.Error(), "HTTP 401") || !strings.Contains(err.Error(), "Bad credentials") {
		t.Fatalf("%v", err)
	}
	// A dead server error does not echo headers or the token either.
	dead := s.host(bbRemote, Credential{Secret: bbToken})
	dead.BaseURL = "http://127.0.0.1:1"
	if _, err := dead.AuthStatus(ctx, ""); err == nil || strings.Contains(err.Error(), bbToken) {
		t.Fatalf("%v", err)
	}
}

func TestBitbucketWithoutACredentialNeverCallsOut(t *testing.T) {
	s := newBB(t, func(bbReq, http.ResponseWriter) { t.Error("no request expected") })
	h := s.host(bbRemote, Credential{})
	h.Creds = nil
	if _, err := h.CreatePR(context.Background(), "", PRRequest{Title: "feat: x"}); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("%v", err)
	}
	if err := h.MergePR(context.Background(), "", PR{ID: "1"}, MergeOptions{Method: "squash"}); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("%v", err)
	}
}
