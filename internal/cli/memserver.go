package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/pgmanaged"
)

// pgManagerFn builds the managed-PostgreSQL manager; tests inject a fake engine.
var pgManagerFn = func(cfg pgmanaged.Config) *pgmanaged.Manager { return pgmanaged.New(cfg) }

const serverTimeout = 10 * time.Minute // an image pull can be slow

func runMemoryServer(env Env, args []string) int {
	return subcommands(env, "memory server", args, map[string]func(Env, []string) int{
		"install": runServerInstall,
		"start":   func(e Env, a []string) int { return runServerSimple(e, a, "start") },
		"stop":    func(e Env, a []string) int { return runServerSimple(e, a, "stop") },
		"status":  runServerStatus,
		"logs":    runServerLogs,
		"backup":  runServerBackup,
		"remove":  runServerRemove,
	})
}

// serverFlags are shared by every `memory server` subcommand.
type serverFlags struct {
	engine   string
	instance string
	port     int
	dryRun   bool
}

func (f *serverFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&f.engine, "engine", "auto", "container engine: docker, podman or auto")
	fs.IntVar(&f.port, "port", 0, "host port on 127.0.0.1 (default: 54329, or the next free one; never 5432)")
	fs.BoolVar(&f.dryRun, "dry-run", false, "show what would happen and change nothing")
	fs.StringVar(&f.instance, "instance", "", "named instance with its own container, volume, network and credentials (default: the standard one)")
}

func serverManager(f serverFlags) (*pgmanaged.Manager, error) {
	renv, err := runtimeEnvFn("")
	if err != nil {
		return nil, err
	}
	cfg := pgmanaged.DefaultConfig(renv.Home)
	cfg.Engine, cfg.Port = f.engine, f.port
	if cfg, err = cfg.WithInstance(f.instance); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return pgManagerFn(cfg), nil
}

func parseServer(env Env, name string, args []string, extra func(*flag.FlagSet)) (*pgmanaged.Manager, serverFlags, int) {
	fs := flag.NewFlagSet("memory server "+name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	var f serverFlags
	f.bind(fs)
	if extra != nil {
		extra(fs)
	}
	if err := fs.Parse(args); err != nil {
		return nil, f, flagExit(err)
	}
	m, err := serverManager(f)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory server %s: %v\n", name, err)
		return nil, f, ExitError
	}
	return m, f, -1
}

func serverFail(env Env, name string, err error) int {
	fmt.Fprintf(env.Stderr, "gabitotl memory server %s: %v\n", name, err)
	if errors.Is(err, pgmanaged.ErrNotManaged) {
		fmt.Fprintln(env.Stderr, "Nothing was changed. GabitoTL only ever touches resources it created and labeled itself.")
	}
	return ExitError
}

func runServerInstall(env Env, args []string) int {
	m, f, code := parseServer(env, "install", args, nil)
	if code >= 0 {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), serverTimeout)
	defer cancel()
	if f.dryRun {
		plan, err := m.Plan(ctx)
		if err != nil {
			return serverFail(env, "install", err)
		}
		fmt.Fprint(env.Stdout, plan.Render())
		fmt.Fprintln(env.Stdout, "Dry run: nothing was changed.")
		if plan.Err() != nil {
			return ExitError
		}
		return ExitOK
	}
	res, err := m.Install(ctx)
	if err != nil {
		return serverFail(env, "install", err)
	}
	verb := "already installed and running"
	if res.Created {
		verb = "installed"
	}
	fmt.Fprintf(env.Stdout, "Managed PostgreSQL %s: %s on %s:%d (loopback only).\n", verb, m.Cfg.Container, pgmanaged.Bind, res.Port)
	fmt.Fprintf(env.Stdout, "Credentials: %s (mode 0600, never printed). Reference them as env:%s.\n", res.SecretsPath, pgmanaged.EnvDatabaseURL)
	inst := ""
	if m.Cfg.Instance != "" {
		inst = " --instance " + m.Cfg.Instance
	}
	fmt.Fprintf(env.Stdout, "Next: gabitotl memory serve --http 127.0.0.1:8765%s, then gabitotl memory token create --user <id>%s.\n", inst, inst)
	return ExitOK
}

func runServerSimple(env Env, args []string, op string) int {
	m, _, code := parseServer(env, op, args, nil)
	if code >= 0 {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), serverTimeout)
	defer cancel()
	var err error
	if op == "start" {
		err = m.Start(ctx)
	} else {
		err = m.Stop(ctx)
	}
	if err != nil {
		return serverFail(env, op, err)
	}
	fmt.Fprintf(env.Stdout, "Managed PostgreSQL %s.\n", map[string]string{"start": "started and ready", "stop": "stopped"}[op])
	return ExitOK
}

func runServerStatus(env Env, args []string) int {
	m, _, code := parseServer(env, "status", args, nil)
	if code >= 0 {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), serverTimeout)
	defer cancel()
	st, err := m.Status(ctx)
	if err != nil {
		return serverFail(env, "status", err)
	}
	yes := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	fmt.Fprintf(env.Stdout, "engine:      %s %s\n", st.Engine.Name, st.Engine.Version)
	fmt.Fprintf(env.Stdout, "installed:   %s\n", yes(st.Installed))
	fmt.Fprintf(env.Stdout, "running:     %s\n", yes(st.Running))
	fmt.Fprintf(env.Stdout, "ready:       %s\n", yes(st.Ready))
	if st.Installed {
		fmt.Fprintf(env.Stdout, "address:     %s:%d\n", pgmanaged.Bind, st.Port)
		fmt.Fprintf(env.Stdout, "image:       %s\n", st.Image)
	}
	fmt.Fprintf(env.Stdout, "data volume: %s\n", yes(st.VolumeOK))
	fmt.Fprintf(env.Stdout, "credentials: %s\n", yes(st.HasSecrets))
	for _, c := range st.Conflicts {
		fmt.Fprintf(env.Stdout, "CONFLICT:    %s\n", c)
	}
	if rep := m.Verify(ctx); st.Installed {
		for _, c := range rep.Checks {
			if c.Status != "ok" {
				fmt.Fprintf(env.Stdout, "[%s] %s: %s\n", c.Status, c.Name, c.Message)
			}
		}
	}
	if !st.Installed || !st.Ready || len(st.Conflicts) > 0 {
		return ExitError
	}
	return ExitOK
}

func runServerLogs(env Env, args []string) int {
	var tail int
	m, _, code := parseServer(env, "logs", args, func(fs *flag.FlagSet) { fs.IntVar(&tail, "tail", 100, "number of lines") })
	if code >= 0 {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := m.Logs(ctx, tail)
	if err != nil {
		return serverFail(env, "logs", err)
	}
	fmt.Fprint(env.Stdout, out)
	return ExitOK
}

func runServerBackup(env Env, args []string) int {
	m, _, code := parseServer(env, "backup", args, nil)
	if code >= 0 {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), serverTimeout)
	defer cancel()
	r, err := m.Backup(ctx)
	if err != nil {
		return serverFail(env, "backup", err)
	}
	fmt.Fprintf(env.Stdout, "Backup written: %s (%d bytes, mode 0600).\n", r.Path, r.Bytes)
	fmt.Fprintln(env.Stdout, "Restore steps are in docs/MEMORY_SERVER.md.")
	return ExitOK
}

func runServerRemove(env Env, args []string) int {
	var purge bool
	m, f, code := parseServer(env, "remove", args, func(fs *flag.FlagSet) {
		fs.BoolVar(&purge, "purge-data", false, "ALSO delete the data volume and credentials (all memory is lost)")
	})
	if code >= 0 {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), serverTimeout)
	defer cancel()
	if f.dryRun {
		st, err := m.Status(ctx)
		if err != nil {
			return serverFail(env, "remove", err)
		}
		if len(st.Conflicts) > 0 {
			for _, c := range st.Conflicts {
				fmt.Fprintf(env.Stdout, "CONFLICT: %s\n", c)
			}
			return ExitError
		}
		fmt.Fprintf(env.Stdout, "Would remove: container %s, network %s (only if they carry the GabitoTL label).\n", m.Cfg.Container, m.Cfg.Network)
		if purge {
			fmt.Fprintf(env.Stdout, "Would DELETE the data volume %s and the credentials file.\n", m.Cfg.Volume)
		} else {
			fmt.Fprintf(env.Stdout, "Would keep the data volume %s.\n", m.Cfg.Volume)
		}
		fmt.Fprintln(env.Stdout, "Dry run: nothing was changed.")
		return ExitOK
	}
	opts := pgmanaged.RemoveOptions{Purge: purge}
	if purge {
		fmt.Fprintf(env.Stdout, "This permanently deletes ALL team memory stored in the volume %s.\nType the volume name to confirm: ", m.Cfg.Volume)
		line, _ := bufio.NewReader(env.Stdin).ReadString('\n')
		if strings.TrimSpace(line) != m.Cfg.Volume {
			fmt.Fprintln(env.Stdout)
			fmt.Fprintln(env.Stderr, "gabitotl memory server remove: confirmation did not match; nothing was removed.")
			return ExitError
		}
		opts.Confirmed = true
	}
	r, err := m.Remove(ctx, opts)
	if err != nil {
		return serverFail(env, "remove", err)
	}
	for _, s := range r.Removed {
		fmt.Fprintf(env.Stdout, "Removed %s.\n", s)
	}
	for _, s := range r.Kept {
		fmt.Fprintf(env.Stdout, "Kept %s.\n", s)
	}
	if len(r.Removed) == 0 && len(r.Kept) == 0 {
		fmt.Fprintln(env.Stdout, "Nothing to remove: the managed PostgreSQL is not installed.")
	}
	return ExitOK
}
