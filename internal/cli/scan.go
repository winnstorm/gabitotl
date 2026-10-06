package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/winnstorm/gabitotl/internal/scanner"
)

func runScan(env Env, args []string) int {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root (default: Git top-level of the current directory)")
	asJSON := fs.Bool("json", false, "print the project map as JSON")
	maxFiles := fs.Int("max-files", scanner.DefaultMaxFiles, "hard cap on files visited")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(env.Stderr, "gabitotl scan: unexpected argument %q\n", fs.Arg(0))
		return ExitUsage
	}
	dir, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl scan: %v\n", err)
		return ExitError
	}
	pm, err := scanner.Scan(context.Background(), scanner.Options{Root: dir, MaxFiles: *maxFiles})
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl scan: %v\n", err)
		return ExitError
	}
	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(pm); err != nil {
			fmt.Fprintf(env.Stderr, "gabitotl scan: %v\n", err)
			return ExitError
		}
		return ExitOK
	}
	fmt.Fprintln(env.Stdout, pm.ToEntry("").Content)
	return ExitOK
}
