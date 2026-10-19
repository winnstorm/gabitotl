package vcs

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
)

// DefaultTokens looks up the stored token of a platform host (the composition
// root points it at the OS credential store). HostFor uses it when HostOptions
// has no Tokens of its own; nil means no stored tokens are handed to gh or glab.
var DefaultTokens func(host string) (token string, ok bool)

// EnvRunner is a Runner that can add KEY=VALUE pairs to the environment of that
// one child process (ExecRunner does).
type EnvRunner interface {
	Runner
	RunEnv(ctx context.Context, dir string, env []string, name string, args ...string) (string, error)
}

// RunEnv implements EnvRunner.
func (r ExecRunner) RunEnv(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	r.Env = append(append([]string(nil), r.Env...), env...)
	return r.Run(ctx, dir, name, args...)
}

// tokenRunner hands the stored token of the remote's host to gh and glab, in that
// child's environment only. Anything already set in the process environment wins.
type tokenRunner struct {
	inner  EnvRunner
	tokens func(host string) (string, bool)
	getenv func(string) string
}

// withStoredTokens wraps r; it returns r unchanged when it cannot carry an
// environment or there is no token source.
func withStoredTokens(r Runner, tokens func(string) (string, bool)) Runner {
	er, ok := runnerOr(r).(EnvRunner)
	if !ok || tokens == nil {
		return r
	}
	return tokenRunner{inner: er, tokens: tokens, getenv: os.Getenv}
}

func (t tokenRunner) Run(ctx context.Context, dir, name string, args ...string) (string, error) {
	if name != "gh" && name != "glab" {
		return t.inner.Run(ctx, dir, name, args...)
	}
	host := DefaultHost(map[string]string{"gh": "github", "glab": "gitlab"}[name])
	if remote, err := t.inner.Run(ctx, dir, "git", "remote", "get-url", "origin"); err == nil && remote != "" {
		if h := remoteHost(remote); h != "" {
			host = h
		}
	}
	token, ok := t.tokens(host)
	env := t.envFor(name, host, token)
	if !ok || len(env) == 0 {
		return t.inner.Run(ctx, dir, name, args...)
	}
	out, err := t.inner.RunEnv(ctx, dir, env, name, args...)
	return scrubSecret(out, token), scrubError(err, token)
}

// secretForms are the spellings a token may take in text: raw, query- and path-escaped.
func secretForms(token string) []string {
	return []string{token, url.QueryEscape(token), url.PathEscape(token)}
}

func scrubSecret(s, token string) string {
	if token == "" {
		return s
	}
	for _, f := range secretForms(token) {
		s = strings.ReplaceAll(s, f, "***")
	}
	return s
}

// scrubError removes the token from err wherever it sits in the chain; when the
// message still carries it afterwards, it is replaced by a plain scrubbed error.
func scrubError(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	var ce *CommandError
	if errors.As(err, &ce) {
		ce.Cmd, ce.Stderr = scrubSecret(ce.Cmd, token), scrubSecret(ce.Stderr, token)
	}
	if msg := err.Error(); scrubSecret(msg, token) != msg {
		return errors.New(scrubSecret(msg, token))
	}
	return err
}

// envFor is the extra environment for name against host, or nil when the person
// already supplied a token through the environment.
func (t tokenRunner) envFor(name, host, token string) []string {
	if token == "" {
		return nil
	}
	set := func(keys ...string) bool {
		for _, k := range keys {
			if t.getenv(k) != "" {
				return true
			}
		}
		return false
	}
	if name == "gh" {
		if host == "github.com" {
			if set("GH_TOKEN", "GITHUB_TOKEN") {
				return nil
			}
			return []string{"GH_TOKEN=" + token}
		}
		if set("GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN") {
			return nil
		}
		return []string{"GH_ENTERPRISE_TOKEN=" + token, "GH_HOST=" + host}
	}
	if set("GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "OAUTH_TOKEN") {
		return nil
	}
	env := []string{"GITLAB_TOKEN=" + token}
	if host != "gitlab.com" {
		env = append(env, "GITLAB_HOST="+host)
	}
	return env
}
