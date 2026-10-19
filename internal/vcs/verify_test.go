package vcs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerifyToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok := false
		switch r.URL.Path {
		case "/user":
			ok = r.Header.Get("Authorization") == "Bearer good" || r.Header.Get("PRIVATE-TOKEN") == "good"
		case "/2.0/user":
			ok = r.Header.Get("Authorization") == "Bearer good"
		}
		if r.URL.Query().Get("boom") != "" || strings.HasPrefix(r.URL.Path, "/500/") {
			w.WriteHeader(500)
			return
		}
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	ctx := context.Background()
	for _, p := range []string{"github", "gitlab", "bitbucket"} {
		st, err := VerifyToken(ctx, p, "", "good", VerifyOptions{APIBase: srv.URL})
		if err != nil || !st.OK() {
			t.Fatalf("%s good: %+v %v", p, st, err)
		}
		st, err = VerifyToken(ctx, p, "", "bad-secret", VerifyOptions{APIBase: srv.URL})
		if err != nil || st.OK() || st.Status != AuthNotLoggedIn {
			t.Fatalf("%s bad: %+v %v", p, st, err)
		}
		if strings.Contains(st.Detail+st.Fix, "bad-secret") {
			t.Fatal("token leaked")
		}
	}
	if st, _ := VerifyToken(ctx, "github", "", "  ", VerifyOptions{}); st.Status != AuthNotLoggedIn {
		t.Fatalf("empty token: %+v", st)
	}
	if _, err := VerifyToken(ctx, "svn", "", "x", VerifyOptions{}); err == nil {
		t.Fatal("unknown platform must fail")
	}
	if _, err := VerifyToken(ctx, "github", "", "x", VerifyOptions{APIBase: srv.URL + "/500"}); err == nil {
		t.Fatal("an HTTP 500 is not a verdict")
	}
}
