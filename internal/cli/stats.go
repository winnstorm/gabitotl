package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"time"

	"github.com/winnstorm/gabitotl/internal/budget"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/stats"
)

// runStats prints what the session costs and what gabitotl kept out of the
// context. It never fails because data is missing: absent files count as zero.
func runStats(env Env, args []string) int {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root (default: Git top-level of the current directory)")
	asJSON := fs.Bool("json", false, "print JSON")
	since := fs.String("since", "", "only savings newer than this (7d, 2w, 12h); default all time")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	r, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl stats: %v\n", err)
		return ExitError
	}
	user := gabitotl.ActiveUserID()
	if user == "" {
		user = activeID(r)
	}
	if user == "" {
		user = "default"
	}
	rep := stats.Collect(gabitotl.NewLayout(r), user, stats.ParseSince(*since), time.Now())
	if *asJSON {
		out, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Fprintln(env.Stdout, string(out))
		return ExitOK
	}
	c := rep.Context
	scope := "all time"
	if rep.Since != "" {
		scope = "last " + rep.Since
	}
	fmt.Fprintf(env.Stdout, "stats for %s (%s; token figures are estimates)\n", rep.User, scope)
	fmt.Fprintf(env.Stdout, "context: %s/%s (%.0f%%, %s), %d checkpoints this session\n",
		kTokens(c.Used), kTokens(c.Limit), c.Percent, c.Source, c.Checkpoints)
	if len(rep.Savings) == 0 {
		fmt.Fprintln(env.Stdout, "savings: none recorded yet (use `gabitotl run -- <cmd>` and `memory context`)")
	}
	for _, k := range rep.Kinds() {
		s := rep.Savings[k]
		fmt.Fprintf(env.Stdout, "savings %s: %d calls, %d B full -> %d B printed, ~%s tokens saved\n",
			k, s.Runs, s.FullBytes, s.PrintedByte, kTokens(s.SavedTokens))
	}
	fmt.Fprintf(env.Stdout, "always-on: ~%d tokens per chat (persona %d + skill descriptions %d)\n",
		rep.AlwaysOn.Total, rep.AlwaysOn.Persona, rep.AlwaysOn.Skills)
	fmt.Fprintf(env.Stdout, "code index: %s\n", rep.Index)
	return ExitOK
}

// recordSavings logs bytes kept out of the context for the active person.
// Best-effort: it needs an initialized project and ignores every failure.
func recordSavings(env Env, flagRoot, kind string, full, printed int) {
	root, err := resolveRoot(env, flagRoot)
	if err != nil {
		return
	}
	user := gabitotl.ActiveUserID()
	if user == "" {
		user = activeID(root)
	}
	if user == "" {
		return
	}
	_ = budget.RecordSavings(gabitotl.NewLayout(root), user, kind, full, printed)
}
