package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"path/filepath"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/identity"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	"github.com/winnstorm/gabitotl/internal/memory/compact"
)

// runMemoryCompact plans (default) or applies a memory compaction. It needs a
// store that lists and soft-deletes, which only the team server's database
// offers; the Engram adapter cannot, so it is refused before anything happens.
func runMemoryCompact(env Env, args []string) int {
	fs := flag.NewFlagSet("memory compact", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root (default: Git top-level of the current directory)")
	older := fs.String("older-than", "30d", "fold operational entries older than this (Nd days or Nw weeks)")
	apply := fs.Bool("apply", false, "write the digests and soft-delete the originals (default: dry run)")
	yes := fs.Bool("yes", false, "with --apply, skip the confirmation")
	asJSON := fs.Bool("json", false, "print the plan as JSON")
	dsn := fs.String("dsn", defaultDSNRef, "database connection, as an env:NAME reference")
	instance := fs.String("instance", "", "named managed instance whose credentials back the default reference")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	age, err := compact.ParseAge(*older)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory compact: %v\n", err)
		return ExitUsage
	}
	r, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory compact: %v\n", err)
		return ExitError
	}
	cfg, err := gabitotl.LoadMemory(gabitotl.NewLayout(r))
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory compact: cannot load .gabitotl/memory.md (run `gabitotl init`): %v\n", err)
		return ExitError
	}
	if cfg.Provider != gabitotl.MemoryMemoryMCP {
		fmt.Fprintf(env.Stderr, "gabitotl memory compact: provider %q cannot delete entries; compaction needs the team memory server (memory-mcp)\n", cfg.Provider)
		return ExitError
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*memoryTimeout)
	defer cancel()
	st, err := openServerStore(ctx, *dsn, *instance)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory compact: %v\n", err)
		return ExitError
	}
	defer st.Close()
	recs, err := compact.List(ctx, st, cfg.ProjectKey)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory compact: %v\n", err)
		return ExitError
	}
	now := time.Now().UTC()
	plan := compact.Build(recs, now, age)
	n, bytes := plan.Folded()
	if *asJSON {
		b, _ := json.MarshalIndent(plan, "", "  ")
		fmt.Fprintln(env.Stdout, string(b))
	} else {
		fmt.Fprintf(env.Stdout, "memory compact (%s, older than %s): %d scanned, %d would be folded (%d bytes), %d stay\n",
			cfg.ProjectKey, *older, plan.Scanned, n, bytes, plan.Kept)
		for _, g := range plan.Groups {
			if g.Kind == "duplicate" {
				fmt.Fprintf(env.Stdout, "  duplicates: %d identical progress entries dropped (newest kept)\n", len(g.Records))
			} else {
				fmt.Fprintf(env.Stdout, "  %s: %d progress entries -> %q\n", g.Month, len(g.Records), g.Digest.Title)
			}
		}
		fmt.Fprintln(env.Stdout, "knowledge entries (decision, bugfix, convention, ...) and topic-keyed entries are never touched.")
	}
	if !*apply {
		if !*asJSON {
			fmt.Fprintln(env.Stdout, "Dry run: nothing changed. Re-run with --apply to fold.")
		}
		return ExitOK
	}
	if n == 0 {
		return ExitOK
	}
	if ok, refused := confirmed(env, ui.Active(""), *yes, fmt.Sprintf("Fold %d entries into digests? Originals are soft-deleted after a backup.", n)); !ok {
		if refused {
			return ExitError
		}
		fmt.Fprintln(env.Stdout, "Nothing changed.")
		return ExitOK
	}
	path, err := compact.Backup(filepath.Join(r, ".gabitotl", ".bak"), now, plan)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory compact: backup failed, nothing changed: %v\n", err)
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "backup: %s\n", path)
	actor := "gabitotl"
	if u, err := identity.Active(r); err == nil && u.ID != "" {
		actor = u.ID
	}
	d, del, err := compact.Apply(ctx, st, plan, actor)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory compact: stopped after %d digests and %d deletions: %v\n", d, del, err)
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "folded: %d digests written, %d entries soft-deleted\n", d, del)
	return ExitOK
}
