package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/identity"
	"github.com/winnstorm/gabitotl/internal/memory/server"
	"github.com/winnstorm/gabitotl/internal/memory/store"
	"github.com/winnstorm/gabitotl/internal/memory/store/pgstore"
	"github.com/winnstorm/gabitotl/internal/pgmanaged"
)

// Composition-root hooks for the memory server; tests replace them.
var (
	openStoreFn = func(ctx context.Context, dsn string) (store.Store, error) {
		return pgstore.Open(ctx, dsn, pgstore.Options{})
	}
	signalCtxFn = func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	}
)

const defaultDSNRef = "env:" + pgmanaged.EnvDatabaseURL

var dsnRefRe = regexp.MustCompile(`^env:([A-Za-z_][A-Za-z0-9_]*)$`)

// openServerStore resolves the connection reference and opens the store. The
// DSN is read from the environment, falling back to the managed credentials
// file for GABITOTL_MEMORY_DATABASE_URL. It is never printed.
func openServerStore(ctx context.Context, ref, instance string) (store.Store, error) {
	m := dsnRefRe.FindStringSubmatch(ref)
	if m == nil {
		return nil, fmt.Errorf("--dsn must be an env:NAME reference, never the connection string itself")
	}
	renv, err := runtimeEnvFn("")
	if err != nil {
		return nil, err
	}
	cfg, err := pgmanaged.DefaultConfig(renv.Home).WithInstance(instance)
	if err != nil {
		return nil, err
	}
	dsn := pgmanaged.LookupSecret(m[1], cfg.SecretsFile(), renv.Getenv)
	if dsn == "" {
		return nil, fmt.Errorf("environment variable %s is not set (and no managed credentials file provides it); run `gabitotl memory server install` or export it", m[1])
	}
	st, err := openStoreFn(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("cannot open the memory database: %w", err)
	}
	return st, nil
}

func runMemoryServe(env Env, args []string) int {
	fs := flag.NewFlagSet("memory serve", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	httpAddr := fs.String("http", "", "serve streamable HTTP on host:port (bearer tokens; loopback by default)")
	stdio := fs.Bool("stdio", false, "serve one local client over stdin/stdout")
	dsn := fs.String("dsn", defaultDSNRef, "database connection, as an env:NAME reference")
	instance := fs.String("instance", "", "named managed instance whose credentials file backs the default reference")
	root := fs.String("root", "", "project root (stdio: source of the default actor and project)")
	actor := fs.String("actor", "", "stdio actor (default: the active identity of the project)")
	project := fs.String("project", "", "default project key for clients that omit it")
	cert := fs.String("tls-cert", "", "TLS certificate (PEM) for HTTP")
	key := fs.String("tls-key", "", "TLS private key (PEM) for HTTP")
	insecure := fs.Bool("allow-insecure-remote", false, "allow plain HTTP on a non-loopback address (only behind a TLS-terminating proxy)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	if *stdio == (*httpAddr != "") {
		fmt.Fprintln(env.Stderr, "Usage: gabitotl memory serve (--http host:port | --stdio) [--dsn env:NAME]")
		return ExitUsage
	}
	ctx, stop := signalCtxFn()
	defer stop()

	cfg := server.Config{DefaultProject: *project}
	if *stdio {
		cfg.FixedActor = *actor
		if r, err := resolveRoot(env, *root); err == nil {
			if cfg.FixedActor == "" {
				if u, err := identity.Active(r); err == nil {
					cfg.FixedActor = u.ID
				}
			}
			if cfg.DefaultProject == "" {
				if mem, err := gabitotl.LoadMemory(gabitotl.NewLayout(r)); err == nil {
					cfg.DefaultProject = mem.ProjectKey
				}
			}
		}
		if cfg.FixedActor == "" {
			fmt.Fprintln(env.Stderr, "gabitotl memory serve: stdio needs an actor: pass --actor, or run inside a project with an active identity")
			return ExitError
		}
	} else if *actor != "" {
		fmt.Fprintln(env.Stderr, "gabitotl memory serve: --actor is for stdio; over HTTP the actor is the owner of the bearer token")
		return ExitUsage
	}
	hc := server.HTTPConfig{Addr: *httpAddr, TLSCert: *cert, TLSKey: *key, AllowInsecureRemote: *insecure}
	if *httpAddr != "" {
		if err := hc.CheckBind(); err != nil {
			fmt.Fprintf(env.Stderr, "gabitotl memory serve: %v\n", err)
			return ExitError
		}
	}

	octx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	st, err := openServerStore(octx, *dsn, *instance)
	cancel()
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory serve: %v\n", err)
		return ExitError
	}
	defer st.Close()
	cfg.Store = st
	// Logs go to stderr: stdout belongs to the protocol in stdio mode.
	cfg.Logger = slog.New(slog.NewJSONHandler(env.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	srv, err := server.New(cfg)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory serve: %v\n", err)
		return ExitError
	}
	if *stdio {
		if err := srv.ServeStdio(ctx); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintf(env.Stderr, "gabitotl memory serve: %v\n", err)
			return ExitError
		}
		return ExitOK
	}
	ln, err := net.Listen("tcp", hc.Addr)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory serve: %v\n", err)
		return ExitError
	}
	scheme := "http"
	if hc.TLSCert != "" {
		scheme = "https"
	}
	fmt.Fprintf(env.Stderr, "gabitotl memory server listening on %s://%s%s\n", scheme, ln.Addr(), server.EndpointPath)
	if err := srv.Serve(ctx, ln, hc); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(env.Stderr, "gabitotl memory serve: %v\n", err)
		return ExitError
	}
	return ExitOK
}

func runMemoryToken(env Env, args []string) int {
	return subcommands(env, "memory token", args, map[string]func(Env, []string) int{
		"create": runTokenCreate,
		"list":   runTokenList,
		"revoke": runTokenRevoke,
	})
}

type tokenFlags struct {
	user, dsn, id, instance string
}

func parseToken(env Env, name string, args []string, withID bool) (store.Store, tokenFlags, func(), int) {
	fs := flag.NewFlagSet("memory token "+name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	var f tokenFlags
	fs.StringVar(&f.user, "user", "", "user id (the actor of everything this token writes)")
	fs.StringVar(&f.dsn, "dsn", defaultDSNRef, "database connection, as an env:NAME reference")
	fs.StringVar(&f.instance, "instance", "", "named managed instance whose credentials file backs the default reference")
	if withID {
		fs.StringVar(&f.id, "id", "", "revoke only this token id (default: every active token of the user)")
	}
	if err := fs.Parse(args); err != nil {
		return nil, f, nil, flagExit(err)
	}
	if name != "list" && f.user == "" {
		fmt.Fprintf(env.Stderr, "Usage: gabitotl memory token %s --user ID [--dsn env:NAME]\n", name)
		return nil, f, nil, ExitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), memoryTimeout*2)
	st, err := openServerStore(ctx, f.dsn, f.instance)
	if err != nil {
		cancel()
		fmt.Fprintf(env.Stderr, "gabitotl memory token %s: %v\n", name, err)
		return nil, f, nil, ExitError
	}
	return st, f, func() { st.Close(); cancel() }, -1
}

func runTokenCreate(env Env, args []string) int {
	st, f, done, code := parseToken(env, "create", args, false)
	if code >= 0 {
		return code
	}
	defer done()
	plain, rec, err := server.NewToken(context.Background(), st, f.user, time.Now())
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory token create: %v\n", err)
		return ExitError
	}
	// Only the token goes to stdout, so it can be captured by a script; the
	// reminder goes to stderr.
	fmt.Fprintln(env.Stdout, plain)
	fmt.Fprintf(env.Stderr, "Token %s for %q created. This is the only time it is shown: store it now (a password manager or a 0600 file).\n", rec.ID, f.user)
	fmt.Fprintf(env.Stderr, "Everything written with it is attributed to %q. Revoke it with: gabitotl memory token revoke --user %s --id %s\n", f.user, f.user, rec.ID)
	return ExitOK
}

func runTokenList(env Env, args []string) int {
	st, f, done, code := parseToken(env, "list", args, false)
	if code >= 0 {
		return code
	}
	defer done()
	ts, err := st.ListTokens(context.Background(), f.user)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory token list: %v\n", err)
		return ExitError
	}
	if len(ts) == 0 {
		fmt.Fprintln(env.Stdout, "no tokens")
		return ExitOK
	}
	for _, t := range ts {
		status := "active"
		if !t.Active() {
			status = "revoked " + t.RevokedAt.UTC().Format("2006-01-02")
		}
		fmt.Fprintf(env.Stdout, "%s  %-20s  created %s  %s\n", t.ID, t.User, t.CreatedAt.UTC().Format("2006-01-02"), status)
	}
	return ExitOK
}

func runTokenRevoke(env Env, args []string) int {
	st, f, done, code := parseToken(env, "revoke", args, true)
	if code >= 0 {
		return code
	}
	defer done()
	ctx := context.Background()
	ids := []string{f.id}
	if f.id == "" {
		ids = nil
		ts, err := st.ListTokens(ctx, f.user)
		if err != nil {
			fmt.Fprintf(env.Stderr, "gabitotl memory token revoke: %v\n", err)
			return ExitError
		}
		for _, t := range ts {
			if t.Active() {
				ids = append(ids, t.ID)
			}
		}
	}
	n := 0
	for _, id := range ids {
		switch err := st.RevokeToken(ctx, f.user, id); {
		case err == nil:
			n++
		case errors.Is(err, store.ErrNotFound):
			fmt.Fprintf(env.Stderr, "gabitotl memory token revoke: no active token %s for %q\n", id, f.user)
			return ExitError
		default:
			fmt.Fprintf(env.Stderr, "gabitotl memory token revoke: %v\n", err)
			return ExitError
		}
	}
	if n == 0 {
		fmt.Fprintf(env.Stdout, "%q has no active tokens.\n", f.user)
		return ExitOK
	}
	fmt.Fprintf(env.Stdout, "Revoked %d token(s) of %q; they stop working immediately.\n", n, f.user)
	return ExitOK
}
