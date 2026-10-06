package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/branding"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/guard"
	"github.com/winnstorm/gabitotl/internal/identity"
	"github.com/winnstorm/gabitotl/internal/persona"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

// executablePath is the gabitotl binary the Git hooks call; tests replace it.
var executablePath = os.Executable

func runGuard(env Env, args []string) int {
	return subcommands(env, "guard", args, map[string]func(Env, []string) int{
		"scan":            runGuardScan,
		"fix":             runGuardFix,
		"install-hooks":   runGuardInstallHooks,
		"uninstall-hooks": runGuardUninstallHooks,
		"status":          runGuardStatus,
		"commit-msg":      runGuardCommitMsg,
	})
}

func guardSettings(root string) (guard.Settings, gabitotl.WorkflowConfig, error) {
	wf, err := workflowConfig(layoutOf(root))
	if err != nil {
		return guard.Settings{}, wf, err
	}
	return guard.Settings{Mode: wf.BrandingGuard, ExtraTerms: wf.BrandingExtraTerms, AllowPaths: wf.BrandingAllowPaths}, wf, nil
}

func gitRunner() vcs.Runner { return vcs.ExecRunner{} }

func printFindings(env Env, fs []branding.Finding) {
	for _, f := range fs {
		key := "guard.finding"
		if f.File == "" {
			key = "guard.finding.nofile"
		}
		fmt.Fprintln(env.Stderr, persona.Msg(key, "file", f.File, "line", strconv.Itoa(f.Line), "class", string(f.Class), "text", f.Text))
	}
}

// reportBranding is wired as Repo.OnBranding: nothing the guard changes is silent.
func reportBranding(env Env) func(op string, removed, warned []branding.Finding) {
	return func(op string, removed, warned []branding.Finding) {
		where := persona.Msg("guard.where.commit")
		if op == vcs.OpPullRequest {
			where = persona.Msg("guard.where.pr")
		}
		if len(removed) > 0 {
			fmt.Fprintln(env.Stderr, persona.Msg("guard.stripped", "n", strconv.Itoa(len(removed)), "where", where))
			printFindings(env, removed)
		}
		if len(warned) > 0 {
			fmt.Fprintln(env.Stderr, persona.Msg("guard.warned", "n", strconv.Itoa(len(warned)), "where", where))
			printFindings(env, warned)
		}
	}
}

type scanReport struct {
	Mode     string                 `json:"mode"`
	Source   string                 `json:"source"`
	Findings []branding.Finding     `json:"findings"`
	History  []guard.HistoryFinding `json:"history,omitempty"`
}

func runGuardScan(env Env, args []string) int {
	fs := flag.NewFlagSet("guard scan", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root (default: Git top-level of the current directory)")
	staged := fs.Bool("staged", false, "scan only the lines the next commit adds")
	diff := fs.String("diff", "", "scan only the lines added in the work tree since REF")
	all := fs.Bool("all", false, "scan every project file (default)")
	history := fs.Bool("history", false, "also audit existing commits (read-only, history is never rewritten)")
	asJSON := fs.Bool("json", false, "print as JSON")
	hook := fs.Bool("hook", false, "run as the pre-commit hook: obey branding_guard")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	n := 0
	for _, b := range []bool{*staged, *diff != "", *all} {
		if b {
			n++
		}
	}
	if n > 1 {
		fmt.Fprintln(env.Stderr, "Usage: gabitotl guard scan [--staged | --diff REF | --all] [--history] [--json]")
		return ExitUsage
	}
	dir, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl guard scan: %v\n", err)
		return ExitError
	}
	s, _, err := guardSettings(dir)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl guard scan: %v\n", err)
		return ExitError
	}
	if *hook && s.Mode == gabitotl.GuardOff {
		return ExitOK
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rep := scanReport{Mode: s.Mode, Source: "all", Findings: []branding.Finding{}}
	switch {
	case *staged:
		rep.Source = "staged"
		rep.Findings, err = guard.ScanStaged(ctx, gitRunner(), dir, s)
	case *diff != "":
		rep.Source = "diff " + *diff
		rep.Findings, err = guard.ScanDiff(ctx, gitRunner(), dir, *diff, s)
	default:
		rep.Findings, err = guard.ScanAll(ctx, gitRunner(), dir, s)
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl guard scan: %v\n", err)
		return ExitError
	}
	if rep.Findings == nil {
		rep.Findings = []branding.Finding{}
	}
	if *history {
		if rep.History, err = guard.ScanHistory(ctx, gitRunner(), dir, s); err != nil {
			fmt.Fprintf(env.Stderr, "gabitotl guard scan: history: %v\n", err)
			return ExitError
		}
	}
	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
	} else {
		printScan(env, rep, *hook)
	}
	if len(rep.Findings) == 0 && len(rep.History) == 0 {
		return ExitOK
	}
	if *hook && len(rep.Findings) > 0 && s.Mode == gabitotl.GuardWarn {
		return ExitOK
	}
	if *hook && len(rep.Findings) == 0 {
		return ExitOK // history is informational in a hook
	}
	return ExitError
}

func printScan(env Env, rep scanReport, hook bool) {
	w := env.Stdout
	if hook {
		w = env.Stderr
	}
	for _, f := range rep.Findings {
		fmt.Fprintf(w, "%s:%d [%s] %s (%s)\n", f.File, f.Line, f.Class, f.Text, f.Action)
	}
	switch {
	case len(rep.Findings) > 0 && hook && rep.Mode == gabitotl.GuardWarn:
		fmt.Fprintln(w, persona.Msg("guard.warned", "n", strconv.Itoa(len(rep.Findings)), "where", persona.Msg("guard.where.staged")))
	case len(rep.Findings) > 0 && hook:
		fmt.Fprintln(w, persona.Msg("guard.blocked", "where", persona.Msg("guard.where.staged")))
		fmt.Fprintln(w, "  gabitotl guard fix --dry-run")
	case len(rep.Findings) > 0:
		fmt.Fprintln(w, persona.Msg("guard.scan.found", "n", strconv.Itoa(len(rep.Findings))))
	case !hook && len(rep.History) == 0:
		fmt.Fprintln(w, persona.Msg("guard.scan.clean"))
	}
	if len(rep.History) > 0 {
		for _, h := range rep.History {
			short := h.Commit
			if len(short) > 8 {
				short = short[:8]
			}
			fmt.Fprintf(w, "history %s (%s) [%s] %s\n", short, h.Where, h.Class, h.Text)
		}
		fmt.Fprintln(w, persona.Msg("guard.history.note", "n", strconv.Itoa(distinctCommits(rep.History))))
	}
}

func distinctCommits(h []guard.HistoryFinding) int {
	seen := map[string]bool{}
	for _, f := range h {
		seen[f.Commit] = true
	}
	return len(seen)
}

func runGuardFix(env Env, args []string) int {
	fs := flag.NewFlagSet("guard fix", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	dry := fs.Bool("dry-run", false, "show the plan, change nothing")
	yes := fs.Bool("yes", false, "apply without asking")
	user := fs.String("user", "", "developer id for the backup (default: active identity)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	dir, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl guard fix: %v\n", err)
		return ExitError
	}
	s, wf, err := guardSettings(dir)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl guard fix: %v\n", err)
		return ExitError
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	files, err := guard.ListFiles(ctx, gitRunner(), dir)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl guard fix: %v\n", err)
		return ExitError
	}
	plan, err := guard.PlanFix(dir, files, s)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl guard fix: %v\n", err)
		return ExitError
	}
	hist, _ := guard.ScanHistory(ctx, gitRunner(), dir, s)
	nlines := 0
	for _, f := range plan.Fixes {
		nlines += len(f.Removed)
	}
	if len(plan.Fixes) == 0 {
		fmt.Fprintln(env.Stdout, persona.Msg("guard.fix.nothing"))
	} else {
		fmt.Fprintln(env.Stdout, persona.Msg("guard.fix.plan", "n", strconv.Itoa(nlines), "files", strconv.Itoa(len(plan.Fixes))))
		for _, f := range plan.Fixes {
			for _, r := range f.Removed {
				fmt.Fprintf(env.Stdout, "  - %s:%d [%s] %s\n", f.Path, r.Line, r.Class, r.Text)
			}
		}
	}
	if len(plan.Manual) > 0 {
		fmt.Fprintln(env.Stdout, persona.Msg("guard.fix.manual", "n", strconv.Itoa(len(plan.Manual))))
		for _, m := range plan.Manual {
			fmt.Fprintf(env.Stdout, "  ? %s:%d [%s] %s\n", m.File, m.Line, m.Class, m.Text)
		}
	}
	if len(hist) > 0 {
		fmt.Fprintln(env.Stdout, persona.Msg("guard.history.note", "n", strconv.Itoa(distinctCommits(hist))))
	}
	if len(plan.Fixes) == 0 || *dry {
		if len(plan.Manual) > 0 {
			return ExitError
		}
		return ExitOK
	}
	if !*yes {
		if !env.IsTTY {
			fmt.Fprintln(env.Stdout, persona.Msg("guard.fix.noconfirm"))
			return ExitError
		}
		p := &prompter{r: bufio.NewReader(env.Stdin), w: env.Stdout}
		if a := strings.ToLower(p.ask(persona.Msg("guard.fix.confirm"), "n")); a != "s" && a != "y" && a != "si" && a != "sí" && a != "yes" {
			fmt.Fprintln(env.Stdout, persona.Msg("guard.fix.noconfirm"))
			return ExitError
		}
	}
	u := *user
	if u == "" {
		if a, err := identity.Active(dir); err == nil {
			u = a.ID
		} else {
			u = "guard"
		}
	}
	res, err := guard.ApplyFix(dir, u, wf.Backup, plan)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl guard fix: %v\n", err)
		return ExitError
	}
	for _, sk := range res.Skipped {
		fmt.Fprintln(env.Stdout, persona.Msg("guard.fix.skipped", "path", sk.Path, "reason", sk.Reason))
	}
	fmt.Fprintln(env.Stdout, persona.Msg("guard.fix.done", "n", strconv.Itoa(nlines), "files", strconv.Itoa(len(res.Written)), "user", u, "snapshot", res.Snapshot))
	if len(plan.Manual) > 0 {
		return ExitError
	}
	return ExitOK
}

func hooksDirFor(env Env, rootFlag string) (root, dir string, err error) {
	root, err = resolveRoot(env, rootFlag)
	if err != nil {
		return "", "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir, err = guard.HooksDir(ctx, gitRunner(), root)
	return root, dir, err
}

// rememberHooks stores the answer in workflow.md (only when the project has one).
func rememberHooks(root, answer string) {
	l := layoutOf(root)
	if _, err := os.Stat(l.Workflow()); err != nil {
		return
	}
	wf, err := gabitotl.LoadWorkflow(l)
	if err != nil || wf.GuardHooks == answer {
		return
	}
	wf.GuardHooks = answer
	_ = gabitotl.SaveWorkflow(l, wf)
}

func runGuardInstallHooks(env Env, args []string) int {
	fs := flag.NewFlagSet("guard install-hooks", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	r, dir, err := hooksDirFor(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl guard install-hooks: %v\n", err)
		return ExitError
	}
	if err := installHooks(env, r, dir); err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl guard install-hooks: %v\n", err)
		return ExitError
	}
	return ExitOK
}

func installHooks(env Env, root, dir string) error {
	return installHooksTo(env.Stdout, root, dir)
}

func installHooksTo(w io.Writer, root, dir string) error {
	bin, err := executablePath()
	if err != nil {
		return err
	}
	st, err := guard.InstallHooks(dir, bin)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, persona.Msg("guard.hooks.installed", "dir", dir))
	for _, h := range st {
		if h.Chained {
			fmt.Fprintln(w, persona.Msg("guard.hooks.chained", "name", h.Name))
		}
	}
	rememberHooks(root, gabitotl.HooksInstalled)
	return nil
}

// ensureHooks is the shared "install by default, remember the answer once" step
// of init and install. It does nothing outside a Git work tree or when the
// answer is already remembered in workflow.md (guard_hooks). decline stores the
// refusal instead of installing.
func ensureHooks(w io.Writer, root string, decline bool) (installed bool, err error) {
	wf, err := gabitotl.LoadWorkflow(layoutOf(root))
	if err != nil || wf.GuardHooks != gabitotl.HooksAsk {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir, err := guard.HooksDir(ctx, gitRunner(), root)
	if err != nil {
		return false, nil // not a Git project: nothing to hook
	}
	if decline {
		rememberHooks(root, gabitotl.HooksDeclined)
		return false, nil
	}
	return true, installHooksTo(w, root, dir)
}

// autoHooks is the installer hook (installer.Env.GuardHooks).
func autoHooks(root string, decline bool) (bool, error) {
	return ensureHooks(io.Discard, root, decline)
}

func runGuardUninstallHooks(env Env, args []string) int {
	fs := flag.NewFlagSet("guard uninstall-hooks", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	r, dir, err := hooksDirFor(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl guard uninstall-hooks: %v\n", err)
		return ExitError
	}
	if _, err := guard.UninstallHooks(dir); err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl guard uninstall-hooks: %v\n", err)
		return ExitError
	}
	fmt.Fprintln(env.Stdout, persona.Msg("guard.hooks.removed", "dir", dir))
	rememberHooks(r, gabitotl.HooksDeclined)
	return ExitOK
}

func runGuardStatus(env Env, args []string) int {
	fs := flag.NewFlagSet("guard status", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	asJSON := fs.Bool("json", false, "print as JSON")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	r, dir, err := hooksDirFor(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl guard status: %v\n", err)
		return ExitError
	}
	s, _, err := guardSettings(r)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl guard status: %v\n", err)
		return ExitError
	}
	st := guard.HookStatus(dir)
	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{"mode": s.Mode, "hooks_dir": dir, "hooks": st})
		return ExitOK
	}
	fmt.Fprintln(env.Stdout, persona.Msg("guard.mode", "mode", s.Mode))
	for _, h := range st {
		state := "no instalado"
		switch {
		case h.Installed && h.Chained:
			state = "instalado (con tu hook encadenado)"
		case h.Installed:
			state = "instalado"
		case h.Foreign:
			state = "hay un hook tuyo, sin el guard"
		}
		fmt.Fprintln(env.Stdout, persona.Msg("guard.hooks.state", "name", h.Name, "state", state))
	}
	return ExitOK
}

// runGuardCommitMsg is the commit-msg hook entry point: strip or refuse
// attribution in the message file Git hands over.
func runGuardCommitMsg(env Env, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(env.Stderr, "Usage: gabitotl guard commit-msg <message-file>")
		return ExitUsage
	}
	dir, err := resolveRoot(env, "")
	if err != nil {
		return ExitOK // not a project: nothing to enforce
	}
	s, _, err := guardSettings(dir)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl guard commit-msg: %v\n", err)
		return ExitError
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl guard commit-msg: %v\n", err)
		return ExitError
	}
	out, removed, warned, err := vcs.Enforce(vcs.BrandingPolicy{Mode: s.Mode, ExtraTerms: s.ExtraTerms}, vcs.OpCommit, string(data))
	if be, ok := vcs.IsBranding(err); ok {
		fmt.Fprintln(env.Stderr, persona.Msg("guard.blocked", "where", persona.Msg("guard.where.commit")))
		printFindings(env, be.Findings)
		return ExitError
	}
	reportBranding(env)(vcs.OpCommit, removed, warned)
	if len(removed) > 0 {
		mode := iofs.FileMode(0o644)
		if st, err := os.Stat(args[0]); err == nil {
			mode = st.Mode().Perm()
		}
		if err := os.WriteFile(args[0], []byte(out+"\n"), mode); err != nil {
			fmt.Fprintf(env.Stderr, "gabitotl guard commit-msg: %v\n", err)
			return ExitError
		}
	}
	return ExitOK
}
