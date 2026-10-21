package vcs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/vcshosts"
)

const bsToken = "bs-s3cr3t-token"

func TestParseServerRemote(t *testing.T) {
	cases := map[string]ServerRepo{
		"https://bb.corp:8443/scm/PROJ/repo.git":           {"PROJ", "repo"},
		"https://user@bb.corp/bitbucket/scm/PROJ/repo.git": {"PROJ", "repo"},
		"https://bb.corp/a/b/scm/proj/my-repo":             {"proj", "my-repo"},
		"https://bb.corp:8443/scm/~jdoe/dotfiles.git":      {"~jdoe", "dotfiles"},
		"ssh://git@bb.corp:7999/proj/repo.git":             {"proj", "repo"},
		"ssh://git@bb.corp:7999/~jdoe/repo.git":            {"~jdoe", "repo"},
		"git@bb.corp:proj/repo.git":                        {"proj", "repo"},
	}
	for in, want := range cases {
		got, err := ParseServerRemote(in)
		if err != nil || got != want {
			t.Errorf("%q: got %+v %v, want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"https://bb.corp/", "git@bb.corp:repo.git", "https://bb.corp/x/y/z/w.git"} {
		if _, err := ParseServerRemote(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

type bsRec struct{ Method, Path, Query, Auth, Body string }

// bsFixture serves a Data Center API under /ctx and records the requests.
func bsFixture(t *testing.T, handle func(r bsRec, w http.ResponseWriter)) (BitbucketServer, *[]bsRec, string) {
	t.Helper()
	var recs []bsRec
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		rec := bsRec{r.Method, strings.TrimPrefix(r.URL.Path, "/ctx"), r.URL.RawQuery, r.Header.Get("Authorization"), b.String()}
		recs = append(recs, rec)
		handle(rec, w)
	}))
	t.Cleanup(srv.Close)
	reg := &vcshosts.Fake{}
	if err := reg.Put(vcshosts.Entry{Platform: vcshosts.BitbucketServer, URL: srv.URL + "/ctx"}); err != nil {
		t.Fatal(err)
	}
	run := &scripted{handle: func(name string, args []string) (string, error) {
		if name == "git" && args[0] == "symbolic-ref" {
			return "feature/x", nil
		}
		return "ssh://git@127.0.0.1:7999/proj/app.git", nil
	}}
	h := BitbucketServer{Runner: run, Servers: reg, Token: func(host string) (string, error) {
		if host != "127.0.0.1" {
			t.Errorf("token asked for %q", host)
		}
		return bsToken, nil
	}}
	return h, &recs, srv.URL
}

func TestBitbucketServerCreatePR(t *testing.T) {
	h, recs, _ := bsFixture(t, func(r bsRec, w http.ResponseWriter) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.Path, "/pull-requests"):
			_, _ = w.Write([]byte(`{"values":[]}`))
		case strings.HasSuffix(r.Path, "/branches/default"):
			_, _ = w.Write([]byte(`{"displayId":"main"}`))
		case r.Method == "POST":
			_, _ = w.Write([]byte(`{"id":7,"version":0,"links":{"self":[{"href":"https://bb/pull-requests/7"}]}}`))
		}
	})
	pr, err := h.CreatePR(context.Background(), "", PRRequest{Title: "feat: x", Body: "b", Draft: true})
	if err != nil || pr.Number != 7 || pr.URL != "https://bb/pull-requests/7" {
		t.Fatalf("got %+v %v", pr, err)
	}
	post := (*recs)[len(*recs)-1]
	if post.Auth != "Bearer "+bsToken || post.Path != "/rest/api/latest/projects/proj/repos/app/pull-requests" {
		t.Fatalf("post: %+v", post)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(post.Body), &body)
	from := body["fromRef"].(map[string]any)
	to := body["toRef"].(map[string]any)
	if from["id"] != "refs/heads/feature/x" || to["id"] != "refs/heads/main" || body["draft"] != true || body["title"] != "feat: x" {
		t.Fatalf("body: %s", post.Body)
	}
}

func TestBitbucketServerExistingPR(t *testing.T) {
	h, recs, _ := bsFixture(t, func(r bsRec, w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"values":[{"id":3,"state":"OPEN","fromRef":{"id":"refs/heads/feature/x"},"links":{"self":[{"href":"https://bb/pr/3"}]}}]}`))
	})
	pr, err := h.CreatePR(context.Background(), "", PRRequest{Title: "t"})
	if err != nil || pr.Number != 3 || len(*recs) != 1 {
		t.Fatalf("got %+v %v (%d requests)", pr, err, len(*recs))
	}
	if q := (*recs)[0].Query; !strings.Contains(q, "state=OPEN") || !strings.Contains(q, "direction=OUTGOING") {
		t.Fatalf("query %q", q)
	}
	if got, ok, err := h.FindPR(context.Background(), "", ""); err != nil || !ok || got.Number != 3 {
		t.Fatalf("FindPR: %+v %v %v", got, ok, err)
	}
}

func TestBitbucketServerMerge(t *testing.T) {
	for method, strategy := range map[string]string{"squash": "squash", "merge": "no-ff", "rebase": "rebase-no-ff"} {
		h, recs, _ := bsFixture(t, func(r bsRec, w http.ResponseWriter) {
			if r.Method == "GET" {
				_, _ = w.Write([]byte(`{"id":5,"version":9,"fromRef":{"id":"refs/heads/feature/x"}}`))
			}
		})
		if err := h.MergePR(context.Background(), "", PR{Number: 5}, MergeOptions{Method: method, DeleteBranch: true}); err != nil {
			t.Fatal(err)
		}
		r := *recs
		if len(r) != 3 || r[1].Method != "POST" || !strings.HasSuffix(r[1].Path, "/pull-requests/5/merge") || r[1].Query != "version=9" {
			t.Fatalf("%s: %+v", method, r)
		}
		var body map[string]any
		_ = json.Unmarshal([]byte(r[1].Body), &body)
		if body["strategyId"] != strategy || body["version"] != float64(9) {
			t.Fatalf("%s body: %s", method, r[1].Body)
		}
		if r[2].Method != "DELETE" || r[2].Path != "/rest/branch-utils/latest/projects/proj/repos/app/branches" || !strings.Contains(r[2].Body, `"refs/heads/feature/x"`) {
			t.Fatalf("delete: %+v", r[2])
		}
	}
}

func TestBitbucketServerMergeConflictAndNoDelete(t *testing.T) {
	h, recs, _ := bsFixture(t, func(r bsRec, w http.ResponseWriter) {
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`{"id":5,"version":1}`))
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"errors":[{"message":"out of date"}]}`))
	})
	err := h.MergePR(context.Background(), "", PR{ID: "5"}, MergeOptions{Method: "squash", DeleteBranch: true})
	if err == nil || !strings.Contains(err.Error(), "HTTP 409: out of date") || len(*recs) != 2 {
		t.Fatalf("got %v (%d)", err, len(*recs))
	}
	if err := h.MergePR(context.Background(), "", PR{ID: "5"}, MergeOptions{Method: "bogus"}); err == nil {
		t.Fatal("unknown method must fail")
	}
}

func TestBitbucketServerSetBase(t *testing.T) {
	h, recs, _ := bsFixture(t, func(r bsRec, w http.ResponseWriter) {
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`{"id":5,"version":4,"title":"keep me"}`))
		}
	})
	if err := h.SetBase(context.Background(), "", PR{Number: 5}, "develop"); err != nil {
		t.Fatal(err)
	}
	put := (*recs)[1]
	var body map[string]any
	_ = json.Unmarshal([]byte(put.Body), &body)
	if put.Method != "PUT" || body["version"] != float64(4) || body["title"] != "keep me" || body["toRef"].(map[string]any)["id"] != "refs/heads/develop" {
		t.Fatalf("put: %+v", put)
	}
}

func TestBitbucketServerChecks(t *testing.T) {
	cases := []struct {
		statuses string
		want     string
	}{
		{`[{"state":"SUCCESSFUL","key":"a"},{"state":"SUCCESSFUL","key":"b"}]`, ChecksPassing},
		{`[{"state":"SUCCESSFUL","key":"a"},{"state":"FAILED","name":"unit"}]`, ChecksFailing},
		{`[{"state":"INPROGRESS","key":"a"}]`, ChecksPending},
		{`[]`, ChecksNone},
	}
	for _, c := range cases {
		h, recs, _ := bsFixture(t, func(r bsRec, w http.ResponseWriter) {
			if strings.Contains(r.Path, "build-status") {
				_, _ = w.Write([]byte(`{"values":` + c.statuses + `}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":5,"version":1,"fromRef":{"latestCommit":"abc123"}}`))
		})
		got, err := h.ChecksState(context.Background(), "", PR{Number: 5})
		if err != nil || got.State != c.want {
			t.Fatalf("%s: %+v %v", c.statuses, got, err)
		}
		if last := (*recs)[len(*recs)-1]; last.Path != "/rest/build-status/latest/commits/abc123" {
			t.Fatalf("path %q", last.Path)
		}
	}
	h, _, _ := bsFixture(t, func(r bsRec, w http.ResponseWriter) { _, _ = w.Write([]byte(`{"id":5,"version":1}`)) })
	if got, _ := h.ChecksState(context.Background(), "", PR{Number: 5}); got.State != ChecksNone {
		t.Fatalf("no head commit: %+v", got)
	}
}

func TestBitbucketServerAuthStatus(t *testing.T) {
	var user string
	h, _, _ := bsFixture(t, func(r bsRec, w http.ResponseWriter) {
		switch {
		case r.Auth != "Bearer "+bsToken:
			w.WriteHeader(401)
		default:
			if user != "" {
				w.Header().Set("X-AUSERNAME", user)
			}
			_, _ = w.Write([]byte(`{}`))
		}
	})
	ctx := context.Background()
	user = "jdoe"
	st, err := h.AuthStatus(ctx, "")
	if err != nil || !st.OK() || !strings.Contains(st.Detail, "jdoe") {
		t.Fatalf("ok: %+v %v", st, err)
	}
	user = "anonymous"
	if st, _ := h.AuthStatus(ctx, ""); st.Status != AuthNotLoggedIn {
		t.Fatalf("anonymous must not count: %+v", st)
	}
	user = ""
	if st, _ := h.AuthStatus(ctx, ""); st.Status != AuthNotLoggedIn {
		t.Fatalf("no X-AUSERNAME must not count: %+v", st)
	}
	h.Token = func(string) (string, error) { return "wrong-token", nil }
	st, err = h.AuthStatus(ctx, "")
	if err != nil || st.Status != AuthNotLoggedIn || strings.Contains(st.Detail+st.Fix, "wrong-token") {
		t.Fatalf("401: %+v %v", st, err)
	}
	h.Token = func(string) (string, error) { return "", nil }
	if st, _ := h.AuthStatus(ctx, ""); st.Status != AuthNotLoggedIn {
		t.Fatalf("no token: %+v", st)
	}
	h.Servers = &vcshosts.Fake{}
	if st, _ := h.AuthStatus(ctx, ""); st.Status != AuthUnsupported {
		t.Fatalf("unregistered: %+v", st)
	}
}

func TestBitbucketServerErrorsNeverCarryTheToken(t *testing.T) {
	h, _, _ := bsFixture(t, func(r bsRec, w http.ResponseWriter) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"errors":[{"message":"boom ` + bsToken + `"}]}`))
	})
	_, err := h.CreatePR(context.Background(), "", PRRequest{Title: "t"})
	if err == nil || strings.Contains(err.Error(), bsToken) {
		t.Fatalf("got %v", err)
	}
	h.Token = func(string) (string, error) { return "", nil }
	if _, err := h.CreatePR(context.Background(), "", PRRequest{Title: "t"}); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("got %v", err)
	}
}

func TestBitbucketServerNeverFollowsRedirects(t *testing.T) {
	leaked := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization") != ""
	}))
	defer other.Close()
	h, _, _ := bsFixture(t, func(r bsRec, w http.ResponseWriter) {})
	// Replace the fixture server by one that redirects.
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound)
	}))
	defer redir.Close()
	reg := &vcshosts.Fake{}
	_ = reg.Put(vcshosts.Entry{Platform: vcshosts.BitbucketServer, URL: redir.URL})
	h.Servers = reg
	if _, err := h.CreatePR(context.Background(), "", PRRequest{Title: "t"}); err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("got %v", err)
	}
	if _, err := VerifyToken(context.Background(), "bitbucket-server", "127.0.0.1", bsToken, VerifyOptions{BaseURL: redir.URL}); err == nil || !strings.Contains(err.Error(), "redirected") {
		t.Fatalf("verify: %v", err)
	}
	if leaked {
		t.Fatal("the token followed a redirect")
	}
}

func TestVerifyTokenBitbucketServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ctx/rest/api/latest/profile/recent/repos" {
			w.WriteHeader(404)
			return
		}
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("X-AUSERNAME", "jdoe")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	ctx := context.Background()
	o := VerifyOptions{BaseURL: srv.URL + "/ctx/"}
	st, err := VerifyToken(ctx, "bitbucket-server", "bb.corp", "good", o)
	if err != nil || !st.OK() || st.User != "jdoe" {
		t.Fatalf("good: %+v %v", st, err)
	}
	st, err = VerifyToken(ctx, "bitbucket-server", "bb.corp", "bad-secret", o)
	if err != nil || st.Status != AuthNotLoggedIn || strings.Contains(st.Detail, "bad-secret") {
		t.Fatalf("bad: %+v %v", st, err)
	}
	if _, err := VerifyToken(ctx, "bitbucket-server", "bb.corp", "good", VerifyOptions{BaseURL: srv.URL}); err == nil || !strings.Contains(err.Error(), "context path") {
		t.Fatalf("404: %v", err)
	}
	if _, err := VerifyToken(ctx, "bitbucket-server", "bb.corp", "good", VerifyOptions{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("no url: %v", err)
	}
}

func TestVerifyTokenBitbucketServerCertificateMessage(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	// The default client trusts only the OS store, not the httptest certificate.
	_, err := VerifyToken(context.Background(), "bitbucket-server", "bb.corp", bsToken, VerifyOptions{BaseURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "certificate not trusted by this system") || strings.Contains(err.Error(), bsToken) {
		t.Fatalf("got %v", err)
	}
	// A client that trusts the server certificate reaches it (anonymous answer: not accepted).
	st, err := VerifyToken(context.Background(), "bitbucket-server", "bb.corp", bsToken, VerifyOptions{BaseURL: srv.URL, HTTP: srv.Client()})
	if err != nil || st.Status != AuthNotLoggedIn {
		t.Fatalf("trusted: %+v %v", st, err)
	}
}

func TestInferProviderWithRegistry(t *testing.T) {
	reg := &vcshosts.Fake{}
	_ = reg.Put(vcshosts.Entry{Platform: vcshosts.BitbucketServer, URL: "https://bitbucket.personal.corp:8443"})
	_ = reg.Put(vcshosts.Entry{Platform: vcshosts.GitHub, URL: "https://ghe.corp"})
	cases := map[string]string{
		"ssh://git@bitbucket.personal.corp:7999/p/r.git":   "bitbucket-server",
		"https://bitbucket.personal.corp:8443/scm/p/r.git": "bitbucket-server",
		"git@ghe.corp:org/r.git":                           "github",
		"git@github.com:org/r.git":                         "github",
		"https://other.corp/x/y.git":                       "git",
		"":                                                 "local",
	}
	for in, want := range cases {
		if got := InferProviderWith(in, reg); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	if got := InferProviderWith("git@ghe.corp:org/r.git", nil); got != "git" {
		t.Errorf("nil registry: %q", got)
	}
}

func TestHostForBitbucketServer(t *testing.T) {
	h, err := HostFor("bitbucket-server", HostOptions{})
	if err != nil || h.(BitbucketServer).Name() != "bitbucket-server" {
		t.Fatalf("%v %v", h, err)
	}
}
