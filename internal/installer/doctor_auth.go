package installer

import (
	"context"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

// vcsRunner adapts the installer's injected Runner to the vcs.Runner the hosts use.
type vcsRunner struct{ env Env }

func (r vcsRunner) Run(ctx context.Context, dir, name string, args ...string) (string, error) {
	out, errb, err := r.env.run(ctx, dir, name, args...)
	if err != nil {
		return string(out), &vcs.CommandError{Cmd: name, Stderr: string(errb), Err: err}
	}
	return string(out), nil
}

// gitAuthLines reports whether the person can act on the Git platform. It is a
// warning with the exact fix, never a failure, and it only applies once the
// delivery settings are answered and the platform is one GabitoTL drives.
// GitHub and GitLab ask their own CLI (`gh auth status`, `glab auth status`);
// Bitbucket is checked offline: it only looks for the token behind the saved
// reference (`gabitotl vcs login` is what verifies it with Bitbucket).
func gitAuthLines(ctx context.Context, env Env, cfg gabitotl.VCSConfig, inferred string) []Line {
	if !cfg.IsConfigured() {
		return nil
	}
	provider := cfg.Provider
	if provider != "github" && provider != "gitlab" && provider != "bitbucket" {
		provider = inferred
	}
	switch provider {
	case "github", "gitlab":
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		var run vcs.Runner = vcsRunner{env}
		if _, real := env.Runner.(ExecRunner); real {
			run = vcs.ExecRunner{} // lets a stored token reach gh and glab (D56)
		}
		h, err := vcs.HostFor(provider, vcs.HostOptions{Runner: run})
		if err != nil {
			return nil
		}
		st, err := h.AuthStatus(ctx, env.ProjectRoot)
		if err != nil {
			return nil
		}
		return authLine(st)
	case "bitbucket":
		lookup := mcp.Resolver{Getenv: env.Getenv, Home: env.Home}.Lookup
		_, err := vcs.ResolveCredential(cfg.Auth, lookup)
		if _, stored := storedVCSToken("bitbucket.org"); err != nil && !stored {
			return []Line{{LineWarn, DocGitAuthTodo, "gabitotl vcs login"}}
		}
		return []Line{{LineOK, DocGitAuth, "bitbucket"}}
	}
	return nil
}

func authLine(st vcs.AuthState) []Line {
	switch st.Status {
	case vcs.AuthLoggedIn:
		return []Line{{LineOK, DocGitAuth, st.Provider}}
	case vcs.AuthToolMissing:
		return []Line{{LineWarn, DocGitAuthTool, st.InstallURL}}
	case vcs.AuthNotLoggedIn:
		return []Line{{LineWarn, DocGitAuthTodo, st.Command}}
	}
	return nil
}

func storedVCSToken(host string) (string, bool) {
	if vcs.DefaultTokens == nil {
		return "", false
	}
	return vcs.DefaultTokens(host)
}
