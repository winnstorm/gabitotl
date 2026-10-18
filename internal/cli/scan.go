package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/winnstorm/gabitotl/internal/identity"
	"github.com/winnstorm/gabitotl/internal/scanner"
)

func runScan(env Env, args []string) int {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root (default: Git top-level of the current directory)")
	asJSON := fs.Bool("json", false, "print the project map as JSON")
	refresh := fs.Bool("refresh", false, "ignore the cached map and rebuild it")
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
	pm, _, err := scanner.Refresh(context.Background(), scanner.Options{Root: dir, MaxFiles: *maxFiles}, scanCache(dir), *refresh)
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

// scanCache is where this person keeps the last project map; "" (no active
// identity, or a project not initialized yet) means every scan is a full one
// and nothing is written.
func scanCache(root string) string {
	if st, err := os.Stat(layoutOf(root).Dir()); err != nil || !st.IsDir() {
		return ""
	}
	a, err := identity.Active(root)
	if err != nil {
		return ""
	}
	p, err := layoutOf(root).LocalScanCache(a.ID)
	if err != nil {
		return ""
	}
	return p
}
