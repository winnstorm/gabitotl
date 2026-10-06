package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/identity"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/migrate"
	"github.com/winnstorm/gabitotl/internal/memory/migrate/engrammcp"
	"github.com/winnstorm/gabitotl/internal/memory/server"
	"github.com/winnstorm/gabitotl/internal/memory/store"
	"github.com/winnstorm/gabitotl/internal/pgmanaged"
	"github.com/winnstorm/gabitotl/internal/version"
)

// Composition-root hooks, replaced by tests so no real home, PATH or command runs.
var (
	installerRunnerFn = func() installer.Runner { return installer.ExecRunner{} }
	installerEnvFn    = func(root string) (installer.Env, error) {
		renv, err := runtimeEnvFn(root)
		if err != nil {
			return installer.Env{}, err
		}
		m := ui.Default()
		return installer.Env{
			Home: renv.Home, ProjectRoot: root, GOOS: renv.GOOS, GOARCH: goruntime.GOARCH,
			Getenv: renv.Getenv, Environ: os.Environ, LookPath: renv.LookPath, Runner: installerRunnerFn(), Now: renv.Now,
			Runtimes: registryFn(), Migrators: migratorRegistry(), Version: version.Version, Team: teamEnv(),
			GuardHooks:   autoHooks,
			OpenMemory:   func(cfg gabitotl.MemoryConfig) (memory.Provider, error) { return newProvider(cfg) },
			RenderReport: ui.ReportRenderer(m),
		}, nil
	}
)

// migratorRegistry holds the tested memory migrators: Engram to the team memory.
func migratorRegistry() *migrate.Registry {
	reg := migrate.NewRegistry()
	_ = reg.Register(engrammcp.New(func(ctx context.Context, instance string) (store.Importer, func(), error) {
		st, err := openServerStore(ctx, defaultDSNRef, instance)
		if err != nil {
			return nil, nil, err
		}
		return st, st.Close, nil
	}))
	return reg
}

// teamEnv wires the installer's team-memory hooks to the managed PostgreSQL, the
// team database and the token minting of the memory server.
func teamEnv() installer.TeamEnv {
	return installer.TeamEnv{
		Manager: func(instance, engine string, port int) (*pgmanaged.Manager, error) {
			if engine == "" {
				engine = "auto"
			}
			return serverManager(serverFlags{engine: engine, instance: instance, port: port})
		},
		OpenStore: func(ctx context.Context, instance string) (store.Store, func(), error) {
			st, err := openServerStore(ctx, defaultDSNRef, instance)
			if err != nil {
				return nil, nil, err
			}
			return st, st.Close, nil
		},
		NewToken: func(ctx context.Context, ts store.TokenStore, user string, projects []string) (string, error) {
			plain, _, err := server.NewScopedToken(ctx, ts, user, projects, false, time.Now())
			return plain, err
		},
	}
}

const installTimeout = 10 * time.Minute

// listFlag is a repeatable flag; values may also be comma-separated.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error {
	*l = append(*l, splitList(v)...)
	return nil
}

type installFlags struct {
	root, memory, user, name                string
	runtimes                                listFlag
	dryRun, yes, noTUI                      bool
	tui                                     bool // the caller allows the full-screen interface
	migratePersona, removeGentle, preserveE bool
	noGuardHooks                            bool

	// Team memory (flags of the plain flow; the TUI asks the same things).
	memoryRole, memoryURL, memoryTokenEnv, memoryEngine, memoryInstance string
	memoryPort                                                          int
	memoryStoreToken, importEngram                                      bool
	memoryInvites                                                       listFlag
}

func (f *installFlags) bind(fs *flag.FlagSet, migrate bool) {
	fs.StringVar(&f.root, "root", "", "project root (default: Git top-level of the current directory)")
	fs.Var(&f.runtimes, "runtime", "runtime to configure (repeatable; default: every detected one)")
	fs.StringVar(&f.memory, "memory", "", "memory provider: engram or memory-mcp (default: Engram when found)")
	fs.StringVar(&f.user, "user", "", "developer id")
	fs.StringVar(&f.name, "name", "", "how Gabito should call you (default: the --user id)")
	fs.BoolVar(&f.dryRun, "dry-run", false, "show the plan and change nothing")
	fs.BoolVar(&f.yes, "yes", false, "do not ask; required when there is no terminal")
	fs.BoolVar(&f.noTUI, "no-tui", false, "use the plain text flow even on a terminal")
	fs.BoolVar(&f.migratePersona, "migrate-persona", false, "retire the managed Gentleman/Gentle-AI persona block after verification")
	fs.StringVar(&f.memoryRole, "memory-role", "", "team memory role with --memory memory-mcp: host (this machine runs the server) or client (connect to one)")
	fs.StringVar(&f.memoryURL, "memory-url", "", "team memory server endpoint, for example https://memoria.example.com/mcp (client; host: the URL teammates use)")
	fs.StringVar(&f.memoryTokenEnv, "memory-token-env", "", "NAME of the environment variable that holds your token (default "+installer.DefaultTokenEnv+"); never the token itself")
	fs.BoolVar(&f.memoryStoreToken, "memory-store-token", false, "client: copy your token from that variable into the 0600 secrets file of your user")
	fs.Var(&f.memoryInvites, "memory-invite", "host: create a token for this teammate id (repeatable; shown once at the end)")
	fs.StringVar(&f.memoryEngine, "memory-engine", "auto", "host: container engine, docker, podman or auto")
	fs.IntVar(&f.memoryPort, "memory-port", 0, "host: loopback port of the managed PostgreSQL (default: automatic)")
	fs.StringVar(&f.memoryInstance, "memory-instance", "", "host: named instance of the managed server")
	fs.BoolVar(&f.importEngram, "import-engram", false, "host: import the Engram memory into the team memory (safety export first; Engram stays intact)")
	fs.BoolVar(&f.noGuardHooks, "no-guard-hooks", false, "do not install the Git hooks of the authorship guard (remembered in workflow.md)")
	fs.BoolVar(&f.removeGentle, "remove-gentle-ai", false, "retire Gentle-AI with its own uninstall, only after every verification passes")
	if migrate {
		fs.BoolVar(&f.preserveE, "preserve-engram", false, "keep using the existing Engram memory (always the case: Engram is adopted, never copied)")
	}
}

func runInstall(env Env, args []string) int {
	var f installFlags
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	f.bind(fs, false)
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(env.Stderr, "gabitotl install: unexpected argument %q\n", fs.Arg(0))
		return ExitUsage
	}
	f.tui = !f.noTUI
	return execute(env, f, installer.SourceInstall)
}

func runMigrate(env Env, args []string) int {
	var f installFlags
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	f.bind(fs, true)
	source := installer.SourceGentleAI // the only harness V1 knows how to migrate from
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		if args[0] != installer.SourceGentleAI {
			fmt.Fprintf(env.Stderr, "gabitotl migrate: sé migrar desde %q (no desde %q)\n", installer.SourceGentleAI, args[0])
			return ExitUsage
		}
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(env.Stderr, "gabitotl migrate: unexpected argument %q\n", fs.Arg(0))
		return ExitUsage
	}
	return execute(env, f, source)
}

func suggestName(ctx context.Context, ienv installer.Env, root string) string {
	if out, _, err := ienv.Runner.Run(ctx, root, "git", "config", "user.name"); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

// openQuestions is what cannot be detected and so must still be asked: the name
// when no identity is active, solo or team and Git automation for a new project.
func openQuestions(root string, inv installer.Inventory, opts installer.Options) ui.Need {
	_, activeErr := identity.Active(root)
	return ui.Need{
		Name: opts.UserName == "" && activeErr != nil,
		Mode: opts.Memory == "" && !inv.Project.Exists,
		Git:  !inv.Project.Exists,
	}
}

// execute is the shared install/migrate flow: detect, ask only what cannot be
// detected, show the plan, and (unless --dry-run) apply it.
func execute(env Env, f installFlags, source string) int {
	m := ui.Default()
	if !f.dryRun && !f.yes && !env.IsTTY {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("refuse.no_tty"))
		return ExitError
	}
	root, err := resolveRoot(env, f.root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "%s %s (%v)\n", ui.SymError, m.Format("refuse.no_project"), err)
		return ExitError
	}
	ienv, err := installerEnvFn(root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl: %v\n", err)
		return ExitError
	}
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	paths := ui.Paths{Home: ienv.Home, Root: root}
	interactive := env.IsTTY && !f.yes && !f.dryRun
	if interactive && f.tui && env.IsOutTTY && source == installer.SourceInstall {
		return executeTUI(env, f, ienv, root, paths)
	}
	var pr *ui.Prompter
	if interactive {
		pr = ui.NewPrompter(env.Stdin, env.Stdout, m)
	}

	inv := installer.BuildInventory(ctx, ienv, nil)
	ui.RenderInventory(env.Stdout, m, inv)
	gentle := inv.Other.GentleAI.Found
	if source == installer.SourceGentleAI && !gentle {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("refuse.no_gentle"))
		return ExitError
	}
	if f.removeGentle && !gentle {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("refuse.remove_needs_gentle"))
		return ExitError
	}
	if source == installer.SourceInstall && gentle && f.removeGentle {
		source = installer.SourceGentleAI
	}
	if source == installer.SourceInstall && gentle && interactive {
		switch pr.ChooseAction(inv) {
		case ui.ActionExit:
			return ExitOK
		case ui.ActionMigrate:
			source = installer.SourceGentleAI
		}
	}

	opts := installer.Options{
		Source: source, Runtimes: f.runtimes, Memory: f.memory, User: f.user, UserName: f.name,
		MigratePersona: f.migratePersona, RemoveGentleAI: f.removeGentle, PreserveEngram: f.preserveE, DryRun: f.dryRun, NoGuardHooks: f.noGuardHooks,
		MemoryRole: f.memoryRole, MemoryURL: f.memoryURL, MemoryTokenEnv: f.memoryTokenEnv, StoreToken: f.memoryStoreToken,
		Invites: f.memoryInvites, ServerEngine: f.memoryEngine, ServerPort: f.memoryPort, ServerInstance: f.memoryInstance,
		ImportEngram: f.importEngram,
	}
	if opts.UserName == "" {
		opts.UserName = f.user
	}

	if interactive {
		pr.Onboard(&opts, openQuestions(root, inv, opts), suggestName(ctx, ienv, root))
	}

	if source == installer.SourceGentleAI {
		ui.RenderOjoMigration(env.Stdout, m, inv)
	}
	plan, err := installer.BuildPlan(ctx, ienv, inv, opts)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl: %v\n", err)
		return ExitUsage
	}
	// Persona conflict: show exactly what would be removed and ask first.
	if interactive && !opts.MigratePersona && plan.HasNote(installer.NotePersonaConflict) {
		po := opts
		po.MigratePersona = true
		if preview, err := installer.BuildPlan(ctx, ienv, inv, po); err == nil {
			blocks := preview.RetireActions()
			if pr.ConfirmPersona(blocks, paths) {
				opts.MigratePersona = true
				if plan, err = installer.BuildPlan(ctx, ienv, inv, opts); err != nil {
					fmt.Fprintf(env.Stderr, "gabitotl: %v\n", err)
					return ExitUsage
				}
			}
		}
	}

	ui.RenderNotes(env.Stdout, m, plan.Notes, inv.Memory.Warnings)
	fmt.Fprintln(env.Stdout)
	ui.RenderPlan(env.Stdout, m, plan, paths)
	if f.dryRun {
		fmt.Fprintf(env.Stdout, "\n%s\n", m.Format(ui.KeyPlanDryDone))
		return ExitOK
	}
	if interactive && !pr.ConfirmApply() {
		return ExitOK
	}

	hooks := installer.Hooks{}
	if interactive {
		hooks.ConfirmRetire = pr.RetireHook()
	}
	fmt.Fprintln(env.Stdout)
	res, err := installer.Run(ctx, ienv, inv, plan, func(e installer.Event) { ui.RenderEvent(env.Stdout, m, e) }, hooks)
	ui.RenderOutcome(env.Stdout, env.Stderr, m, res, inv, paths, err)
	if err != nil {
		return ExitError
	}
	return ExitOK
}
