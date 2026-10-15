package cli

import (
	"context"
	"errors"
	"io/fs"
	"net/http"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

// Composition-root hooks for the platform commands, replaced by tests so no real
// gh, glab, terminal or Bitbucket server is ever involved.
var (
	vcsRunnerFn = func() vcs.Runner { return vcs.ExecRunner{} }
	vcsTTYFn    = func() vcs.TTYRunner { return vcs.ExecTTY{} }
	// vcsHTTPFn and vcsAPIFn override the Bitbucket client and API root ("" and nil keep the real ones).
	vcsHTTPFn = func() *http.Client { return nil }
	vcsAPIFn  = func() string { return "" }
)

// platformNames are the providers GabitoTL can drive, with their display names.
var platformNames = map[string]string{"github": "GitHub", "gitlab": "GitLab", "bitbucket": "Bitbucket"}

// platformTool is the command-line tool that owns the login of each CLI-based platform.
var platformTool = map[string]string{"github": "gh", "gitlab": "glab"}

// platformOf returns the platform to act on: the one vcs.md names when it is one
// GabitoTL can drive, otherwise the one origin points at ("git" or "local" when
// there is none).
func platformOf(ctx context.Context, dir string, cfg gabitotl.VCSConfig) string {
	if _, ok := platformNames[cfg.Provider]; ok {
		return cfg.Provider
	}
	st, err := vcs.New(dir, cfg).Status(ctx)
	if err != nil || st.Provider == "" {
		return cfg.Provider
	}
	return st.Provider
}

// tokenLookup resolves a variable the way the memory clients do: the process
// environment, then the 0600 secrets file of the user.
func tokenLookup(root string) func(string) string {
	renv, err := runtimeEnvFn(root)
	if err != nil {
		return func(string) string { return "" }
	}
	return mcp.Resolver{Getenv: renv.Getenv, Home: renv.Home}.Lookup
}

// hostFor builds the host of provider for the person's saved auth reference.
func hostFor(provider, root string, cfg gabitotl.VCSConfig) (vcs.Host, error) {
	lookup := tokenLookup(root)
	return vcs.HostFor(provider, vcs.HostOptions{
		Runner: vcsRunnerFn(), HTTP: vcsHTTPFn(), BitbucketAPI: vcsAPIFn(),
		Credentials: func() (vcs.Credential, error) { return vcs.ResolveCredential(cfg.Auth, lookup) },
	})
}

// authOf reports the login state of the platform of this project; ok is false
// when the provider is not one GabitoTL can drive.
func authOf(ctx context.Context, root string, cfg gabitotl.VCSConfig) (vcs.AuthState, bool) {
	provider := platformOf(ctx, root, cfg)
	if _, ok := platformNames[provider]; !ok {
		return vcs.AuthState{Provider: provider}, false
	}
	h, err := hostFor(provider, root, cfg)
	if err != nil {
		return vcs.AuthState{Provider: provider}, false
	}
	st, err := h.AuthStatus(ctx, root)
	if err != nil { // a transport failure is not a verdict on the login
		return vcs.AuthState{Provider: provider, Status: vcs.AuthNotLoggedIn, Detail: err.Error(),
			Fix: "Could not check the login (" + err.Error() + "); retry, or run: gabitotl vcs login"}, true
	}
	return st, true
}

// loadVCSOrDefault is the merged settings of the active person, or the defaults
// when the project has no vcs.md yet.
func loadVCSOrDefault(layout gabitotl.Layout, user string) (gabitotl.VCSConfig, error) {
	cfg, err := gabitotl.LoadVCSFor(layout, user)
	if errors.Is(err, fs.ErrNotExist) {
		return gabitotl.DefaultVCS("git"), nil
	}
	return cfg, err
}
