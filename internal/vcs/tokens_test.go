package vcs

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type envFake struct {
	remote string
	calls  [][]string // env per gh/glab call
	fail   string     // stderr of a failing gh/glab call
}

func (f *envFake) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	if name == "git" {
		return f.remote, nil
	}
	f.calls = append(f.calls, nil)
	return "", nil
}

func (f *envFake) RunEnv(_ context.Context, _ string, env []string, name string, _ ...string) (string, error) {
	f.calls = append(f.calls, env)
	if f.fail != "" {
		return "", &CommandError{Cmd: name, Stderr: f.fail, Err: errors.New("exit 1")}
	}
	return "ok", nil
}

func runWith(f *envFake, set map[string]string, tokens map[string]string, name string) (string, error) {
	tr := withStoredTokens(f, func(h string) (string, bool) { t, ok := tokens[h]; return t, ok }).(tokenRunner)
	tr.getenv = func(k string) string { return set[k] }
	return tr.Run(context.Background(), "", name, "auth", "status")
}

func TestStoredTokenInjectedOnlyWhenMissing(t *testing.T) {
	toks := map[string]string{"github.com": "T1", "ghe.corp": "T2", "gitlab.com": "T3", "gl.corp": "T4"}
	cases := []struct {
		name, remote string
		env          map[string]string
		want         string // joined env, "" = none
	}{
		{"gh", "git@github.com:a/b.git", nil, "GH_TOKEN=T1"},
		{"gh", "git@github.com:a/b.git", map[string]string{"GITHUB_TOKEN": "x"}, ""},
		{"gh", "https://ghe.corp/a/b.git", nil, "GH_ENTERPRISE_TOKEN=T2 GH_HOST=ghe.corp"},
		{"gh", "https://ghe.corp/a/b.git", map[string]string{"GH_ENTERPRISE_TOKEN": "x"}, ""},
		{"glab", "git@gitlab.com:a/b.git", nil, "GITLAB_TOKEN=T3"},
		{"glab", "https://gl.corp/a/b.git", nil, "GITLAB_TOKEN=T4 GITLAB_HOST=gl.corp"},
		{"glab", "https://gl.corp/a/b.git", map[string]string{"GITLAB_TOKEN": "x"}, ""},
		{"gh", "https://other.example/a/b.git", nil, ""}, // no stored token for that host
	}
	for _, c := range cases {
		f := &envFake{remote: c.remote}
		if _, err := runWith(f, c.env, toks, c.name); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(f.calls[len(f.calls)-1], " "); got != c.want {
			t.Errorf("%s %s %v: env %q, want %q", c.name, c.remote, c.env, got, c.want)
		}
	}
}

func TestStoredTokenNeverLeaksIntoErrors(t *testing.T) {
	f := &envFake{remote: "git@github.com:a/b.git", fail: "bad credentials for SECRET-TOKEN"}
	_, err := runWith(f, nil, map[string]string{"github.com": "SECRET-TOKEN"}, "gh")
	if err == nil || strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Fatalf("error must exist and be scrubbed: %v", err)
	}
}

func TestRunnerWithoutEnvSupportIsLeftAlone(t *testing.T) {
	var plain Runner = noRunner{}
	if withStoredTokens(plain, func(string) (string, bool) { return "t", true }) != plain {
		t.Fatal("a runner that cannot carry an environment must not be wrapped")
	}
}
