package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os/exec"
	"strings"

	"github.com/winnstorm/gabitotl/internal/codeindex"
)

func runIndex(env Env, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(env.Stderr, "Usage: gabitotl index <status|setup> [flags]")
		return ExitUsage
	}
	switch args[0] {
	case "status":
		return runIndexStatus(env, args[1:])
	case "setup":
		return runIndexSetup(env, args[1:])
	}
	fmt.Fprintf(env.Stderr, "gabitotl index: unknown subcommand %q (want status or setup)\n", args[0])
	return ExitUsage
}

func runIndexStatus(env Env, args []string) int {
	fs := flag.NewFlagSet("index status", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root (default: Git top-level of the current directory)")
	asJSON := fs.Bool("json", false, "print as JSON")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	r, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl index status: %v\n", err)
		return ExitError
	}
	statuses := map[string]codeindex.Status{}
	for _, b := range codeindex.All() {
		statuses[b.Name()] = b.Detect(r)
	}
	next := codeindex.NextStep(r)
	if *asJSON {
		out, _ := json.MarshalIndent(map[string]any{"backends": statuses, "next": next}, "", "  ")
		fmt.Fprintln(env.Stdout, string(out))
		return ExitOK
	}
	for _, b := range codeindex.All() {
		fmt.Fprintf(env.Stdout, "%s: %s\n", b.Name(), codeindex.Describe(statuses[b.Name()]))
	}
	fmt.Fprintf(env.Stdout, "next: %s\n", next)
	return ExitOK
}

func runIndexSetup(env Env, args []string) int {
	fs := flag.NewFlagSet("index setup", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root (default: Git top-level of the current directory)")
	tool := fs.String("tool", "", "codegraph or code-review-graph (default: the first one installed)")
	yes := fs.Bool("yes", false, "run the command; without it only print it")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	r, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl index setup: %v\n", err)
		return ExitError
	}
	var b codeindex.Backend
	if *tool != "" {
		var ok bool
		if b, ok = codeindex.ByName(*tool); !ok {
			fmt.Fprintf(env.Stderr, "gabitotl index setup: unknown tool %q (want codegraph or code-review-graph)\n", *tool)
			return ExitUsage
		}
	} else if b = codeindex.Default(r); b == nil {
		fmt.Fprintf(env.Stderr, "gabitotl index setup: no code index tool is installed; %s\n", codeindex.CodeGraph().InstallHint())
		return ExitError
	}
	if !b.Detect(r).Installed {
		fmt.Fprintf(env.Stderr, "gabitotl index setup: %s is not installed; %s\n", b.Name(), b.InstallHint())
		return ExitError
	}
	argv := b.SetupCommand(r)
	if !*yes {
		fmt.Fprintf(env.Stdout, "would run (in %s): %s\nrun again with --yes to do it\n", r, strings.Join(argv, " "))
		return ExitOK
	}
	cmd := exec.Command(argv[0], argv[1:]...) // argument list, no shell
	cmd.Dir, cmd.Stdout, cmd.Stderr = r, env.Stdout, env.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl index setup: %s failed: %v\n", argv[0], err)
		return ExitError
	}
	return ExitOK
}

// indexLine is the one informational doctor line about the code index.
func indexLine(root string) string {
	parts := make([]string, 0, 2)
	for _, b := range codeindex.All() {
		parts = append(parts, b.Name()+" "+codeindex.Describe(b.Detect(root)))
	}
	return "code index (optional): " + strings.Join(parts, "; ")
}
