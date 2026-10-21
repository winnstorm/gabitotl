package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/credstore"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	"github.com/winnstorm/gabitotl/internal/vcs"
	"github.com/winnstorm/gabitotl/internal/vcshosts"
)

// Self-hosted servers (Bitbucket Data Center, GitHub Enterprise, self-managed
// GitLab) live in a non-secret registry; their tokens live in the credential
// store, keyed by host (docs/DECISIONS.md D60).

// serversFn builds the registry of the user at home; tests replace it.
var serversFn = func(home string) vcshosts.Registry { return vcshosts.NewFile(home) }

func servers() vcshosts.Registry { return serversFn(userHome()) }

// defaultTokenEnvServer is the variable `vcs login --url` reads the token from.
const defaultTokenEnvServer = "BITBUCKET_SERVER_TOKEN"

const vcsServerTimeout = 45 * time.Second

// serverTokenFor resolves the token of a Data Center host: the environment
// variable named by the person's own auth reference (env:NAME) when it is set,
// otherwise the credential store by host (the port is not part of the key).
func serverTokenFor(root string, cfg gabitotl.VCSConfig) func(host string) (string, error) {
	return func(host string) (string, error) {
		if name, ok := strings.CutPrefix(strings.Trim(strings.TrimSpace(cfg.Auth), `"'`), "env:"); ok && name != "" {
			if v := strings.TrimSpace(tokenLookup(root)(name)); v != "" {
				return v, nil
			}
		}
		tok, ok, err := credStoreFn(userHome()).Get(host)
		if err != nil || !ok {
			return "", nil
		}
		return tok, nil
	}
}

// publicPlatformOf is the platform of a public host ("" for any other).
func publicPlatformOf(host string) string {
	for _, p := range []string{"github", "gitlab", "bitbucket"} {
		if vcs.DefaultHost(p) == host {
			return p
		}
	}
	return ""
}

// verifyAt checks token against platform at base (the registry URL; "" for a public host).
func verifyAt(ctx context.Context, platform, host, base, token string) (vcs.AuthState, error) {
	return vcs.VerifyToken(ctx, platform, host, token, vcs.VerifyOptions{HTTP: vcsHTTPFn(), APIBase: vcsAPIFn(), BaseURL: base})
}

// reportVerdict prints the outcome of a token check; it returns ExitOK when accepted.
func reportVerdict(env Env, m ui.Messages, host string, st vcs.AuthState, err error) int {
	switch {
	case err != nil:
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.srv.unreachable", "host", host, "detail", err.Error()))
		return ExitError
	case !st.OK():
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.srv.rejected", "host", host))
		return ExitError
	case st.User != "":
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("vcs.srv.ok.user", "host", host, "user", st.User))
	default:
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("vcs.srv.ok", "host", host))
	}
	return ExitOK
}

// loginServer registers a self-hosted server and stores its token, but only after
// the server accepted the token. The token comes from an environment variable
// (--token-env, default BITBUCKET_SERVER_TOKEN), from standard input
// (--token-stdin) or, failing both, the one already stored for the host; it is
// never read from an argument and never printed. With check nothing is written.
func loginServer(ctx context.Context, env Env, m ui.Messages, rawURL, platform, tokenEnv string, fromStdin, check bool) int {
	fail := func(key string, pairs ...string) int {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format(key, pairs...))
		return ExitError
	}
	if platform == "" {
		platform = vcshosts.BitbucketServer
	}
	if !vcshosts.ValidPlatform(platform) {
		return fail("vcs.srv.badplatform", "platform", platform)
	}
	base, host, err := vcshosts.ParseBaseURL(rawURL)
	if err != nil {
		return fail("vcs.srv.badurl", "detail", strings.TrimPrefix(err.Error(), "vcshosts: "))
	}
	token, stored := "", false
	switch {
	case fromStdin:
		line, _ := bufio.NewReader(env.Stdin).ReadString('\n')
		token = strings.TrimSpace(line)
	default:
		name := tokenEnv
		if name == "" {
			name = defaultTokenEnvServer
		}
		if !envNameRe.MatchString(name) {
			return fail("vcs.login.bb.badname", "name", name)
		}
		token = strings.TrimSpace(tokenLookup("")(name))
		if token == "" {
			if tok, ok, _ := credStoreFn(userHome()).Get(host); ok {
				token, stored = tok, true
			}
		}
	}
	if !credstore.ValidToken(token) {
		return fail("vcs.srv.notoken", "name", orDefault(tokenEnv, defaultTokenEnvServer))
	}
	st, verr := verifyAt(ctx, platform, host, base, token)
	if code := reportVerdict(env, m, host, st, verr); code != ExitOK || check {
		return code
	}
	if err := servers().Put(vcshosts.Entry{Host: host, Platform: platform, URL: base}); err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs login: register server: %v\n", err)
		return ExitError
	}
	if !stored {
		if err := credStoreFn(userHome()).Set(host, token); err != nil {
			fmt.Fprintf(env.Stderr, "gabitotl vcs login: store token: %v\n", err)
			return ExitError
		}
	}
	fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("vcs.srv.stored", "host", host, "url", base, "target", credstore.Target(host)))
	return ExitOK
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// loginServerProject is `vcs login` inside a project whose remote is a registered
// Data Center server: it checks the token the project would use.
func loginServerProject(ctx context.Context, env Env, m ui.Messages, dir string, cfg gabitotl.VCSConfig) int {
	st, ok := authOf(ctx, dir, cfg)
	if !ok {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.login.unsupported", "platform", "Bitbucket Server"))
		return ExitError
	}
	if st.Status == vcs.AuthUnsupported {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.srv.unregistered"))
		return ExitError
	}
	if st.OK() {
		fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("vcs.login.ok", "platform", "Bitbucket Server"))
		return ExitOK
	}
	fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.srv.login.todo"))
	return ExitError
}

// runVCSTest checks the stored token of a host against its server, on demand.
func runVCSTest(env Env, args []string) int {
	fl := flag.NewFlagSet("vcs test", flag.ContinueOnError)
	fl.SetOutput(env.Stderr)
	root := fl.String("root", "", "project root (default: Git top-level of the current directory)")
	host := fl.String("host", "", "host to test (default: the host of this project's origin remote)")
	if err := fl.Parse(args); err != nil {
		return flagExit(err)
	}
	m := ui.Active("")
	h := credstore.NormalizeHost(*host)
	if h == "" {
		dir, err := resolveRoot(env, *root)
		if err != nil {
			fmt.Fprintf(env.Stderr, "gabitotl vcs test: %v (or pass --host)\n", err)
			return ExitError
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		remote, rerr := vcs.ExecRunner{}.Run(ctx, dir, "git", "remote", "get-url", "origin")
		cancel()
		if rerr != nil || strings.TrimSpace(remote) == "" {
			fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.srv.nohost"))
			return ExitError
		}
		h = credstore.NormalizeHost(vcs.RemoteHost(remote))
	}
	platform, base := publicPlatformOf(h), ""
	if e, ok := vcshosts.Lookup(servers(), h); ok {
		platform, base = e.Platform, e.URL
	}
	if platform == "" {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.srv.unregistered"))
		return ExitError
	}
	token, ok, err := credStoreFn(userHome()).Get(h)
	if err != nil || !ok {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.srv.nostored", "host", h))
		return ExitError
	}
	ctx, cancel := context.WithTimeout(context.Background(), vcsServerTimeout)
	defer cancel()
	st, verr := verifyAt(ctx, platform, h, base, token)
	return reportVerdict(env, m, h, st, verr)
}
