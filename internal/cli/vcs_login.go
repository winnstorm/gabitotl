package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/credstore"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

const loginTimeout = 5 * time.Minute

// runVCSLogin checks the login to the Git platform and, when there is a terminal,
// helps with it: GitHub and GitLab through their own CLI (gh / glab), Bitbucket
// Cloud through a token that lives in an environment variable. With --check it
// only verifies. Without a terminal it never prompts: it prints the exact fix and
// exits non-zero.
func runVCSLogin(env Env, args []string) int {
	fl := flag.NewFlagSet("vcs login", flag.ContinueOnError)
	fl.SetOutput(env.Stderr)
	root := fl.String("root", "", "project root (default: Git top-level of the current directory)")
	tokenEnv := fl.String("token-env", "", "Bitbucket: name of the environment variable that holds the token (default BITBUCKET_TOKEN)")
	store := fl.Bool("store-token", false, "Bitbucket: copy the token from that variable into the credential store of your user")
	check := fl.Bool("check", false, "only verify the login; never log in or change anything")
	if err := fl.Parse(args); err != nil {
		return flagExit(err)
	}
	m := ui.Active("")
	dir, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs login: %v\n", err)
		return ExitError
	}
	layout := gabitotl.NewLayout(dir)
	user := gabitotl.ActiveUserID()
	cfg, err := loadVCSOrDefault(layout, user)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs login: %v\n", err)
		return ExitError
	}
	ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	defer cancel()
	provider := platformOf(ctx, dir, cfg)
	name, known := platformNames[provider]
	if !known {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.login.noplatform", "provider", provider))
		return ExitError
	}
	if provider == "bitbucket" {
		return loginBitbucket(ctx, env, m, dir, layout, user, cfg, *tokenEnv, *store, *check)
	}
	h, err := hostFor(provider, dir, cfg)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs login: %v\n", err)
		return ExitError
	}
	st, err := h.AuthStatus(ctx, dir)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs login: %v\n", err)
		return ExitError
	}
	switch st.Status {
	case vcs.AuthLoggedIn:
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("vcs.login.ok", "platform", name))
		return ExitOK
	case vcs.AuthToolMissing:
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.login.toolmissing", "tool", platformTool[provider], "url", st.InstallURL))
		return ExitError
	case vcs.AuthUnsupported:
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.login.unsupported", "platform", name))
		return ExitError
	}
	if *check {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.login.notlogged", "platform", name, "command", st.Command))
		return ExitError
	}
	if !env.IsTTY {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.login.notty", "platform", name, "command", st.Command))
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymTodo, m.Format("vcs.login.running", "platform", name, "command", st.Command))
	tool, rest, _ := strings.Cut(st.Command, " ")
	if err := vcsTTYFn().RunTTY(ctx, dir, tool, strings.Fields(rest)...); err != nil {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.login.failed", "platform", name, "command", st.Command))
		return ExitError
	}
	if again, err := h.AuthStatus(ctx, dir); err != nil || !again.OK() {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.login.failed", "platform", name, "command", st.Command))
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("vcs.login.done", "platform", name))
	return ExitOK
}

// loginBitbucket verifies the token behind an env reference and, unless only
// checking, saves the reference (never the token) in the person's own vcs.md.
func loginBitbucket(ctx context.Context, env Env, m ui.Messages, dir string, layout gabitotl.Layout, user string, cfg gabitotl.VCSConfig, tokenEnv string, store, check bool) int {
	fail := func(key string, pairs ...string) int {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format(key, pairs...))
		return ExitError
	}
	prompter := ui.NewPrompter(env.Stdin, env.Stdout, m)
	interactive := env.IsTTY && !check
	nameVar := tokenEnv
	if nameVar == "" {
		if n, err := vcs.TokenEnvName(cfg.Auth); err == nil {
			nameVar = n
		} else {
			nameVar = vcs.DefaultBitbucketTokenEnv
		}
		if interactive && cfg.Auth == "" {
			nameVar = prompter.Ask(m.Format("vcs.login.bb.askenv"), nameVar)
		}
	}
	if !envNameRe.MatchString(nameVar) {
		return fail("vcs.login.bb.badname", "name", nameVar)
	}
	renv, err := runtimeEnvFn(dir)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs login: %v\n", err)
		return ExitError
	}
	fromEnv := renv.Getenv(nameVar)
	if store && !check {
		if fromEnv == "" {
			return fail("vcs.login.bb.notset", "name", nameVar)
		}
		return finishBitbucket(ctx, env, m, dir, layout, user, cfg, nameVar, fromEnv, true, check)
	}
	value := tokenLookup(dir)(nameVar)
	if value == "" {
		return fail("vcs.login.bb.notset", "name", nameVar)
	}
	offer := interactive && fromEnv != "" && mcp.Resolver{Getenv: func(string) string { return "" }, Home: renv.Home}.Lookup(nameVar) != fromEnv
	return finishBitbucket(ctx, env, m, dir, layout, user, cfg, nameVar, value, offer && prompter.Confirm(m.Format("vcs.login.bb.askstore")), check)
}

func finishBitbucket(ctx context.Context, env Env, m ui.Messages, dir string, layout gabitotl.Layout, user string, cfg gabitotl.VCSConfig, nameVar, value string, store, check bool) int {
	ref := "env:" + nameVar
	trial := cfg
	trial.Auth = ref
	st, ok := authWithToken(ctx, dir, trial, value)
	switch {
	case !ok:
		fmt.Fprintf(env.Stderr, "gabitotl vcs login: %s\n", st.Detail)
		return ExitError
	case st.Status == vcs.AuthUnsupported:
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.login.unsupported", "platform", "Bitbucket"))
		return ExitError
	case !st.OK():
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.login.bb.rejected", "name", nameVar))
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("vcs.login.bb.ok", "name", nameVar))
	if check {
		return ExitOK
	}
	if store {
		if err := credStoreFn(userHome()).Set("bitbucket.org", value); err != nil {
			fmt.Fprintf(env.Stderr, "gabitotl vcs login: store token: %v\n", err)
			return ExitError
		}
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("vcs.login.bb.stored", "target", credstore.Target("bitbucket.org")))
	}
	if !gabitotl.Exists(dir) {
		return ExitOK // nothing to save the reference in; the login itself works
	}
	if cfg.Auth != ref {
		cfg.Auth = ref
		if err := gabitotl.SaveVCSFor(layout, user, cfg); err != nil {
			fmt.Fprintf(env.Stderr, "gabitotl vcs login: %v\n", err)
			return ExitError
		}
	}
	if p, err := layout.LocalVCS(user); err == nil {
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("vcs.login.bb.saved", "ref", ref, "path", p))
	}
	return ExitOK
}

// authWithToken checks value against Bitbucket without touching the saved
// reference: the credential is the one given, not the one in the settings.
func authWithToken(ctx context.Context, dir string, cfg gabitotl.VCSConfig, value string) (vcs.AuthState, bool) {
	h, err := vcs.HostFor("bitbucket", vcs.HostOptions{
		Runner: vcsRunnerFn(), HTTP: vcsHTTPFn(), BitbucketAPI: vcsAPIFn(),
		Credentials: func() (vcs.Credential, error) {
			return vcs.ResolveCredential(cfg.Auth, func(string) string { return value })
		},
	})
	if err != nil {
		return vcs.AuthState{Detail: err.Error()}, false
	}
	st, err := h.AuthStatus(ctx, dir)
	if err != nil {
		return vcs.AuthState{Detail: err.Error()}, false
	}
	return st, true
}
