package cli

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/persona"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
	"github.com/winnstorm/gabitotl/internal/runtime/all"
)

// Composition-root hooks, replaced by tests so no real home or PATH is read.
var (
	registryFn   = all.Registry
	runtimeEnvFn = func(root string) (rt.Env, error) { return rt.OSEnv(goruntime.GOOS, root) }
)

func runRuntime(env Env, args []string) int {
	return subcommands(env, "runtime", args, map[string]func(Env, []string) int{
		"list":   runRuntimeList,
		"detect": runRuntimeDetect,
		"plan":   func(e Env, a []string) int { return runRuntimeApply(e, a, "plan") },
		"apply":  func(e Env, a []string) int { return runRuntimeApply(e, a, "apply") },
		"verify": func(e Env, a []string) int { return runRuntimeApply(e, a, "verify") },
		"remove": runRuntimeRemove,
		"set":    runRuntimeSet,
	})
}

// rtFlags are shared by the runtime subcommands.
type rtFlags struct {
	root, runtimes, scope, memory string
	migrate                       bool
}

func (f *rtFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&f.root, "root", "", "project root (default: Git top-level of the current directory)")
	fs.StringVar(&f.runtimes, "runtime", "", "comma-separated runtimes (default for plan/detect: every detected one)")
	fs.StringVar(&f.scope, "scope", "global", "where to configure: global or project")
	fs.StringVar(&f.memory, "memory", "", "memory MCP to configure: engram, memory-mcp or none; both register the same project-routing bridge (default: from .gabitotl/memory.md)")
	fs.BoolVar(&f.migrate, "migrate-persona", false, "allow removing the managed Gentleman/Gentle-AI persona block (after GabitoTL is verified)")
}

// projectRoot returns the project root or "" when not inside a project.
func projectRoot(env Env, flagRoot string) string {
	if r, err := resolveRoot(env, flagRoot); err == nil {
		return r
	}
	return ""
}

// memoryMCP resolves the memory MCP entries to configure. The provider of the
// project only validates the choice: both providers are served by the same
// bridge entry. Engram's own entry is added when its binary is on PATH.
func memoryMCP(root, choice string, engramPresent bool) (rt.MemoryMCP, []rt.MemoryMCP, []string) {
	if choice == "" && root != "" {
		if m, err := gabitotl.LoadMemory(gabitotl.NewLayout(root)); err == nil {
			choice = m.Provider
		}
	}
	return rt.MemoryFor(choice, engramPresent)
}

// pick returns the adapters named in csv, or (when empty and detected is true)
// every adapter that detects something on this machine.
func pick(reg *rt.Registry, renv rt.Env, csv string, detected bool) ([]rt.Adapter, error) {
	if csv == "" {
		var out []rt.Adapter
		for _, a := range reg.All() {
			if !detected || a.Detect(renv).State != rt.StateNone {
				out = append(out, a)
			}
		}
		return out, nil
	}
	var out []rt.Adapter
	for _, n := range splitList(csv) {
		a, ok := reg.Get(n)
		if !ok {
			return nil, fmt.Errorf("unknown runtime %q (known: %s)", n, strings.Join(reg.Names(), ", "))
		}
		out = append(out, a)
	}
	return out, nil
}

func (f *rtFlags) setup(env Env, name string) (rt.Env, rt.Options, string, error) {
	root := projectRoot(env, f.root)
	if f.scope != string(rt.ScopeGlobal) && f.scope != string(rt.ScopeProject) {
		return rt.Env{}, rt.Options{}, "", fmt.Errorf("--scope must be global or project")
	}
	if f.scope == string(rt.ScopeProject) && root == "" {
		return rt.Env{}, rt.Options{}, "", fmt.Errorf("--scope project needs a project (run `gabitotl init` first)")
	}
	renv, err := runtimeEnvFn(root)
	if err != nil {
		return rt.Env{}, rt.Options{}, "", err
	}
	_, engramPresent := renv.Look("engram")
	mem, extra, retire := memoryMCP(root, f.memory, engramPresent)
	opts := rt.Options{Scope: rt.Scope(f.scope), PersonaText: persona.TextFor(gabitotl.ActivePersonaLang(renv.Home)), Skills: installer.SkillAssets(), Memory: mem, MemoryExtra: extra, RetireMemory: retire, MigrateGentleman: f.migrate}
	return renv, opts, root, nil
}

// modelFor returns the project model only for the runtime it was chosen for.
func modelFor(root string, a rt.Adapter) string {
	if root == "" {
		return ""
	}
	r, err := rt.ResolveRuntime(gabitotl.NewLayout(root), activeID(root))
	if err != nil || r.Runtime != a.Name() {
		return ""
	}
	return r.Model
}

func runRuntimeList(env Env, args []string) int {
	fs := flag.NewFlagSet("runtime list", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	cur := rt.Resolved{}
	if r := projectRoot(env, *root); r != "" {
		cur, _ = rt.ResolveRuntime(gabitotl.NewLayout(r), activeID(r))
	}
	for _, a := range registryFn().All() {
		mark := " "
		if a.Name() == cur.Runtime {
			mark = "*"
		}
		fmt.Fprintf(env.Stdout, "%s %-12s %s\n", mark, a.Name(), a.Label())
	}
	if cur.Runtime != "" {
		fmt.Fprintf(env.Stdout, "selected: %s (%s)", cur.Runtime, cur.RuntimeSource)
		if cur.Model != "" {
			fmt.Fprintf(env.Stdout, ", model %s (%s)", cur.Model, cur.ModelSource)
		}
		fmt.Fprintln(env.Stdout)
	}
	return ExitOK
}

func runRuntimeDetect(env Env, args []string) int {
	fs := flag.NewFlagSet("runtime detect", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	var f rtFlags
	f.bind(fs)
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	renv, _, _, err := f.setup(env, "detect")
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl runtime detect: %v\n", err)
		return ExitError
	}
	list, err := pick(registryFn(), renv, f.runtimes, false)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl runtime detect: %v\n", err)
		return ExitUsage
	}
	for _, a := range list {
		d := a.Detect(renv)
		fmt.Fprintf(env.Stdout, "%s: %s\n", a.Label(), d.State)
		for _, s := range d.Signals {
			mark := "-"
			if s.Found {
				mark = "+"
			}
			fmt.Fprintf(env.Stdout, "  %s %s %s\n", mark, s.Name, s.Detail)
		}
	}
	return ExitOK
}

// runRuntimeApply implements plan (dry), apply and verify.
func runRuntimeApply(env Env, args []string, mode string) int {
	fs := flag.NewFlagSet("runtime "+mode, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	var f rtFlags
	f.bind(fs)
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	fail := func(err error) int {
		fmt.Fprintf(env.Stderr, "gabitotl runtime %s: %v\n", mode, err)
		return ExitError
	}
	if mode == "apply" && f.runtimes == "" {
		fmt.Fprintln(env.Stderr, "gabitotl runtime apply: name the runtimes with --runtime (use `runtime plan` to preview)")
		return ExitUsage
	}
	renv, opts, root, err := f.setup(env, mode)
	if err != nil {
		return fail(err)
	}
	list, err := pick(registryFn(), renv, f.runtimes, true)
	if err != nil {
		return fail(err)
	}
	if len(list) == 0 {
		fmt.Fprintln(env.Stdout, "no runtimes detected")
		return ExitOK
	}
	if opts.Memory.Command == "" {
		fmt.Fprintln(env.Stderr, "note: no memory MCP configured (memory provider has no selected MCP server, or --memory none)")
	}
	code := ExitOK
	for _, a := range list {
		o := opts
		o.Model = modelFor(root, a)
		fmt.Fprintf(env.Stdout, "%s\n", a.Label())
		switch mode {
		case "verify":
			for _, c := range a.Verify(renv, o) {
				fmt.Fprintf(env.Stdout, "  %s %s %s\n", okMark(c.OK), c.Name, c.Detail)
				if !c.OK {
					code = ExitError
				}
			}
			continue
		}
		plan, err := a.Plan(renv, o)
		if err != nil {
			return fail(err)
		}
		for _, ac := range plan {
			fmt.Fprintf(env.Stdout, "  %s\n", ac)
		}
		if mode == "plan" {
			continue
		}
		if c := applyOne(env.Stdout, renv, a, plan); c != ExitOK {
			code = c
		}
	}
	return code
}

func okMark(ok bool) string {
	if ok {
		return "ok  "
	}
	return "FAIL"
}

func applyOne(w io.Writer, renv rt.Env, a rt.Adapter, plan []rt.Action) int {
	snap := &rt.Snapshot{Dir: filepath.Join(renv.StateDir, "backups", renv.Clock().Format("20060102-150405"), a.Name())}
	if err := a.Backup(renv, snap, plan); err != nil {
		fmt.Fprintf(w, "  backup failed: %v (nothing was changed)\n", err)
		return ExitError
	}
	if len(snap.Files) > 0 {
		fmt.Fprintf(w, "  backup: %d file(s) in %s\n", len(snap.Files), snap.Dir)
	}
	res, err := a.Apply(renv, plan)
	for _, ac := range res.Applied {
		fmt.Fprintf(w, "  applied %s\n", ac)
	}
	for _, ac := range res.Removed {
		fmt.Fprintf(w, "  removed %s\n", ac)
	}
	for _, ac := range res.Manual {
		fmt.Fprintf(w, "  MANUAL  %s\n", ac)
	}
	if err != nil {
		fmt.Fprintf(w, "  error: %v\n", err)
		return ExitError
	}
	return ExitOK
}

func runRuntimeRemove(env Env, args []string) int {
	fs := flag.NewFlagSet("runtime remove", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	var f rtFlags
	f.bind(fs)
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	if f.runtimes == "" {
		fmt.Fprintln(env.Stderr, "gabitotl runtime remove: name the runtimes with --runtime")
		return ExitUsage
	}
	renv, _, _, err := f.setup(env, "remove")
	if err == nil {
		var list []rt.Adapter
		if list, err = pick(registryFn(), renv, f.runtimes, false); err == nil {
			for _, a := range list {
				res, rerr := a.RemoveGabitoConfig(renv)
				for _, ac := range res.Removed {
					fmt.Fprintf(env.Stdout, "%s: cleaned %s\n", a.Name(), ac.Path)
				}
				for _, ac := range res.Manual {
					fmt.Fprintf(env.Stdout, "%s: MANUAL %s\n", a.Name(), ac)
				}
				if rerr != nil {
					err = rerr
					break
				}
			}
		}
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl runtime remove: %v\n", err)
		return ExitError
	}
	return ExitOK
}

func runRuntimeSet(env Env, args []string) int {
	fs := flag.NewFlagSet("runtime set", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	name := fs.String("runtime", "", "runtime to select")
	model := fs.String("model", "", "model to select (runtime and model are independent)")
	local := fs.Bool("local", false, "store as a local override (.gabitotl/local/runtime.md, never committed)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	dir, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl runtime set: %v\n", err)
		return ExitError
	}
	if *name != "" {
		if _, ok := registryFn().Get(*name); !ok {
			fmt.Fprintf(env.Stderr, "gabitotl runtime set: unknown runtime %q (known: %s)\n", *name, strings.Join(registryFn().Names(), ", "))
			return ExitUsage
		}
	}
	layout := gabitotl.NewLayout(dir)
	if err := rt.SetRuntime(layout, activeID(dir), *name, *model, *local); err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl runtime set: %v\n", err)
		return ExitError
	}
	r, _ := rt.ResolveRuntime(layout, activeID(dir))
	fmt.Fprintf(env.Stdout, "runtime: %s (%s)\n", r.Runtime, r.RuntimeSource)
	if r.Model != "" {
		fmt.Fprintf(env.Stdout, "model: %s (%s)\n", r.Model, r.ModelSource)
	}
	fmt.Fprintln(env.Stdout, "persona, memory and workflow files were not touched; run `gabitotl runtime plan` to see what the change means for the runtime config")
	return ExitOK
}
