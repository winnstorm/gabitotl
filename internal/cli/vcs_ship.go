package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/installer/ui"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

// runVCSShip runs the whole delivery from the saved settings: commit when dirty,
// push, pull request, merge when permitted. It prints one line per step; --json
// prints the result instead and --dry-run changes nothing.
func runVCSShip(env Env, args []string) int {
	fl := flag.NewFlagSet("vcs ship", flag.ContinueOnError)
	fl.SetOutput(env.Stderr)
	root := fl.String("root", "", "project root (default: Git top-level of the current directory)")
	msg := fl.String("m", "", "commit message for uncommitted changes, a Conventional Commit")
	title := fl.String("title", "", "pull request title (required)")
	body := fl.String("body", "", "pull request description")
	dry := fl.Bool("dry-run", false, "print the plan and change nothing")
	asJSON := fl.Bool("json", false, "print the result as JSON")
	wait := fl.Duration("wait", defaultMergeWait, "how long to wait for the checks before merging")
	if err := fl.Parse(args); err != nil {
		return flagExit(err)
	}
	d, code := openDelivery(env, "vcs ship", *root)
	if d == nil {
		return code
	}
	if strings.TrimSpace(*title) == "" {
		d.warn("deliver.pr.notitle")
		return ExitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), *wait+30*time.Minute)
	defer cancel()
	var h vcs.Host
	if !*dry {
		if h, code = d.host(ctx); h == nil {
			return code
		}
	}
	family := "deliver.step."
	if *dry {
		family = "deliver.plan."
		if !*asJSON {
			fmt.Fprintln(env.Stdout, d.m.Format("deliver.plan.header"))
		}
	}
	res, err := d.repo.Ship(ctx, vcs.ShipOptions{
		Message: *msg, Title: *title, Body: *body, DryRun: *dry, Host: h, Wait: *wait, Waiter: deliveryWaiterFn(),
		OnStep: func(s vcs.Step) {
			if !*asJSON {
				d.printStep(family, s)
			}
		},
	})
	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	}
	if err != nil {
		return d.fail(err)
	}
	return ExitOK
}

// printStep words one ship step.
func (d *delivery) printStep(family string, s vcs.Step) {
	var pairs []string
	for k, v := range s.Args {
		pairs = append(pairs, k, v)
	}
	key := family + s.Code
	switch {
	case s.Code == "stop":
		key = "deliver.stop." + s.Args["reason"]
	case strings.HasPrefix(s.Code, "note-"):
		key = "deliver.note." + strings.TrimPrefix(s.Code, "note-")
	}
	fmt.Fprintf(d.env.Stdout, "%s %s\n", ui.SymOK, d.m.Format(key, pairs...))
}
