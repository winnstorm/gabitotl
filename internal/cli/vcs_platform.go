package cli

import (
	"context"
	"errors"
	"io/fs"
	"net/http"

	"github.com/winnstorm/gabitotl/internal/credstore"
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
	// credStoreFn is the OS credential store of the user at home (Windows Credential
	// Manager, or a 0600 file elsewhere); tests replace it with a fake.
	credStoreFn = func(home string) credstore.Store { return credstore.New(home) }
)

// userHome is the home folder of the user, "" when unknown.
func userHome() string {
	if renv, err := runtimeEnvFn(""); err == nil {
		return renv.Home
	}
	return ""
}

// credentialFor resolves the Bitbucket credential in this order: the process
// environment and the secrets file behind the person's auth reference (env:NAME),
// then the credential store by host (bitbucket.org). Only a missing credential
// falls through; any other error is returned.
func credentialFor(root string, cfg gabitotl.VCSConfig) (vcs.Credential, error) {
	c, err := vcs.ResolveCredential(cfg.Auth, tokenLookup(root))
	if err == nil || !errors.Is(err, vcs.ErrNotLoggedIn) {
		return c, err
	}
	if tok, ok, serr := credStoreFn(userHome()).Get("bitbucket.org"); serr == nil && ok {
		return vcs.ParseCredential(tok), nil
	}
	return c, err
}

// platformNames are the providers GabitoTL can drive, with their display names.
var platformNames = map[string]string{"github": "GitHub", "gitlab": "GitLab", "bitbucket": "Bitbucket", "bitbucket-server": "Bitbucket Server"}

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
	return vcs.HostFor(provider, vcs.HostOptions{
		Runner: vcsRunnerFn(), HTTP: vcsHTTPFn(), BitbucketAPI: vcsAPIFn(),
		Credentials: func() (vcs.Credential, error) { return credentialFor(root, cfg) },
		Servers:     servers(), ServerToken: serverTokenFor(root, cfg),
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

// loadVCSOrDefault is the merged settings of the active person: built-in
// defaults, then the user's global ~/.gabitotl/vcs.md, then the project team file
// and the person's own file. A project without vcs.md yet gets the global layer.
func loadVCSOrDefault(layout gabitotl.Layout, user string) (gabitotl.VCSConfig, error) {
	base, _, err := gabitotl.LoadGlobalVCS(userHome())
	if err != nil {
		return base, err
	}
	cfg, err := gabitotl.LoadVCSLayered(layout, user, base)
	if errors.Is(err, fs.ErrNotExist) {
		return base, nil
	}
	return cfg, err
}

func init() {
	// Hand the stored token of the remote's host to gh and glab (D56).
	vcs.DefaultTokens = func(host string) (string, bool) {
		tok, ok, err := credStoreFn(userHome()).Get(host)
		return tok, ok && err == nil
	}
	vcs.DefaultServers = servers
}
