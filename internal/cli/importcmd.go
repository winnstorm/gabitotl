package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/importer"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/memory/providers"
)

func runImport(env Env, args []string) int {
	return subcommands(env, "import", args, map[string]func(Env, []string) int{
		"detect": func(e Env, a []string) int { return runImportRun(e, a, true) },
		"run":    func(e Env, a []string) int { return runImportRun(e, a, false) },
	})
}

// engramProjectsFn is replaced by tests so the real engram CLI is never run.
var engramProjectsFn = providers.EngramProjects

func importEnv(env Env, root string) (importer.Env, error) {
	renv, err := runtimeEnvFn(root)
	if err != nil {
		return importer.Env{}, err
	}
	ie := importer.Env{Home: renv.Home, ProjectRoot: root, Getenv: renv.Getenv, LookPath: renv.LookPath, Now: renv.Now}
	if m, err := gabitotl.LoadMemory(gabitotl.NewLayout(root)); err == nil {
		ie.ProjectKey = m.ProjectKey
	}
	ie.Projects = func(ctx context.Context) ([]importer.ProjectInfo, error) {
		ps, err := engramProjectsFn(ctx)
		out := make([]importer.ProjectInfo, len(ps))
		for i, p := range ps {
			out[i] = importer.ProjectInfo{Name: p.Name, Observations: p.Observations}
		}
		return out, err
	}
	return ie, nil
}

func runImportRun(env Env, args []string, detectOnly bool) int {
	name := "import run"
	if detectOnly {
		name = "import detect"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	source := fs.String("source", "", "only this source: "+strings.Join(importer.Names(), ", "))
	dry := fs.Bool("dry-run", false, "show what would be imported and write nothing")
	force := fs.Bool("force", false, "import again even when the source is unchanged")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	dir, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl %s: %v\n", name, err)
		return ExitError
	}
	var only []string
	if *source != "" {
		if !contains(importer.Names(), *source) {
			fmt.Fprintf(env.Stderr, "gabitotl %s: unknown source %q (want one of %s)\n", name, *source, strings.Join(importer.Names(), ", "))
			return ExitUsage
		}
		only = []string{*source}
	}
	ienv, err := importEnv(env, dir)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl %s: %v\n", name, err)
		return ExitError
	}
	if detectOnly {
		for _, s := range importer.Sources() {
			if len(only) > 0 && s.Name() != only[0] {
				continue
			}
			d := s.Detect(ienv)
			fmt.Fprintf(env.Stdout, "%s: %s\n", s.Name(), map[bool]string{true: "found", false: "not found"}[d.Found])
			for _, sg := range d.Signals {
				fmt.Fprintf(env.Stdout, "  %s %s %s\n", map[bool]string{true: "+", false: "-"}[sg.Found], sg.Name, sg.Detail)
			}
		}
		return ExitOK
	}
	opts := importer.Options{Only: only, DryRun: *dry, Force: *force}
	layout := gabitotl.NewLayout(dir)
	if !*dry {
		if sink, err := memorySink(env, dir); err != nil {
			fmt.Fprintf(env.Stderr, "note: memory unavailable (%v); recording imports.md only\n", err)
		} else {
			opts.Sink = sink
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), memoryTimeout)
	defer cancel()
	out, err := importer.Run(ctx, layout, ienv, opts)
	for _, o := range out {
		fmt.Fprintf(env.Stdout, "%s: %s", o.Source, o.Status)
		if o.Hash != "" {
			fmt.Fprintf(env.Stdout, " (%d items, %s)", len(o.Items), o.Hash)
		}
		fmt.Fprintln(env.Stdout)
		if o.Status == importer.StatusWouldApply || o.Status == importer.StatusImported {
			for _, k := range o.Items {
				fmt.Fprintf(env.Stdout, "  - %s: %s (%s)\n", k.Kind, k.Title, k.Path)
			}
		}
		for _, s := range o.Skipped {
			fmt.Fprintf(env.Stdout, "  skipped %s\n", s)
		}
		if o.Err != "" {
			fmt.Fprintf(env.Stderr, "  error: %s\n", o.Err)
		}
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl %s: %v\n", name, err)
		return ExitError
	}
	if !*dry {
		fmt.Fprintf(env.Stdout, "registry: %s\n", layout.Imports())
	}
	return ExitOK
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// memorySink saves imported knowledge through the configured provider.
func memorySink(env Env, root string) (func(context.Context, importer.Knowledge) error, error) {
	p, cfg, err := openMemory(env, root)
	if err != nil {
		return nil, err
	}
	return installer.KnowledgeSink(p, cfg), nil
}
