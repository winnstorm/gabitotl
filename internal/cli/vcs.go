package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

func runVCS(env Env, args []string) int {
	return subcommands(env, "vcs", args, map[string]func(Env, []string) int{
		"status": runVCSStatus, "branch": runVCSBranch, "setup": runVCSSetup, "login": runVCSLogin,
		"commit": runVCSCommit, "push": runVCSPush, "pr": runVCSPR, "merge": runVCSMerge, "ship": runVCSShip,
	})
}

// boolPtr is a flag.Value for an optional bool: unset stays nil, a bare flag is
// true and --flag=false is false. It lets "not answered" differ from "no".
type boolPtr struct{ p **bool }

func (b boolPtr) String() string {
	if b.p == nil || *b.p == nil {
		return ""
	}
	return strconv.FormatBool(**b.p)
}

func (b boolPtr) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	*b.p = &v
	return nil
}

func (boolPtr) IsBoolFlag() bool { return true }

// deliveryFlags are the --vcs-* flags shared by `install`, `migrate` and
// `vcs setup`. Giving --vcs-automation settles the delivery questions without a
// terminal.
type deliveryFlags struct {
	provider, automation, mergeMethod string
	deleteBranch, draft               *bool
}

func (f *deliveryFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&f.provider, "vcs-provider", "", "platform of the repository: github, gitlab, bitbucket (default: inferred from origin)")
	fs.StringVar(&f.automation, "vcs-automation", "", "what Gabito does alone: all (commit, push, PR, merge), until-pr (you merge) or ask; settles the delivery questions")
	fs.StringVar(&f.mergeMethod, "vcs-merge-method", "", "how a PR is merged: squash, merge or rebase (default squash)")
	fs.Var(boolPtr{&f.deleteBranch}, "vcs-delete-branch", "delete the branch after the merge (--vcs-delete-branch=false to keep it)")
	fs.Var(boolPtr{&f.draft}, "vcs-draft", "open PRs as drafts (--vcs-draft=false for normal PRs)")
}

// answers turns the flags into delivery answers, rejecting unknown values.
func (f deliveryFlags) answers() (gabitotl.DeliveryAnswers, error) {
	a := gabitotl.DeliveryAnswers{
		Provider: f.provider, Automation: f.automation, MergeMethod: f.mergeMethod,
		DeleteBranch: f.deleteBranch, Draft: f.draft,
	}
	return a, a.Validate()
}

func yesNo(m ui.Messages, v bool) string {
	if v {
		return m.Format("vcs.setup.yes")
	}
	return m.Format("vcs.setup.no")
}

// printDeliverySummary says in one line what the delivery settings amount to.
func printDeliverySummary(w io.Writer, m ui.Messages, c gabitotl.VCSConfig) {
	fmt.Fprintf(w, "%s %s\n", ui.SymOK, m.Format("vcs.setup.summary",
		"provider", c.Provider, "automation", m.Format("vcs.setup.automation."+c.AutomationLevel()),
		"merge", c.EffectiveMergeMethod(), "delete", yesNo(m, c.DeleteBranch), "draft", yesNo(m, c.DraftPRs)))
}

// runVCSSetup asks the delivery questions on demand (also when they were already
// answered) and saves the answers. Where they go is gabitotl.SaveDelivery's rule:
// the team file for a new or unconfigured project or for team-wide settings, your
// own file when the team already answered and you only choose how much to automate.
func runVCSSetup(env Env, args []string) int {
	var df deliveryFlags
	fl := flag.NewFlagSet("vcs setup", flag.ContinueOnError)
	fl.SetOutput(env.Stderr)
	root := fl.String("root", "", "project root (default: Git top-level of the current directory)")
	yes := fl.Bool("yes", false, "do not ask; keep the current values unless flags were given")
	shared := fl.Bool("shared", false, "write the team file even when the team already answered (changes it for everyone)")
	df.bind(fl)
	if err := fl.Parse(args); err != nil {
		return flagExit(err)
	}
	m := ui.Default()
	dir, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs setup: %v\n", err)
		return ExitError
	}
	layout := gabitotl.NewLayout(dir)
	if !gabitotl.Exists(dir) {
		fmt.Fprintf(env.Stderr, "gabitotl vcs setup: %s\n", "there is no .gabitotl here: run gabitotl install first")
		return ExitError
	}
	da, err := df.answers()
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs setup: %v\n", err)
		return ExitUsage
	}
	da.Shared = *shared
	user := gabitotl.ActiveUserID()
	cfg, err := gabitotl.LoadVCSFor(layout, user)
	if errors.Is(err, fs.ErrNotExist) {
		cfg, err = gabitotl.DefaultVCS("git"), nil
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs setup: %v\n", err)
		return ExitError
	}
	interactive := env.IsTTY && !*yes
	if interactive && da.Automation == "" {
		inferred := ""
		if st, err := vcs.New(dir, cfg).Status(context.Background()); err == nil {
			inferred = st.Provider
		}
		need := ui.Need{
			Delivery: true, Inferred: inferred,
			DeliveryFull: !cfg.IsConfigured() || da.Shared || da.TeamLevel(),
		}
		ui.NewPrompter(env.Stdin, env.Stdout, m).Delivery(&da, need)
	}
	adoptInferredProvider(dir, cfg, &da)
	if da.Empty() {
		printDeliverySummary(env.Stdout, m, cfg)
		if !env.IsTTY && !*yes {
			fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("vcs.setup.nothing"))
			return ExitError
		}
		return ExitOK
	}
	res, err := gabitotl.SaveDelivery(layout, user, da)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs setup: %v\n", err)
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "%s %s\n", ui.SymOK, m.Format("vcs.setup.saved."+res.Target, "path", res.Path))
	printDeliverySummary(env.Stdout, m, res.Config)
	return ExitOK
}

// adoptInferredProvider fills in the platform when nobody chose one: if the
// answers carry no provider and the configured one is the generic "git" (or none),
// the platform origin points at is adopted, so a GitLab or Bitbucket remote is not
// left as "git". An explicit choice is never touched. It only applies when
// something else was answered, so a bare `--yes` still changes nothing.
func adoptInferredProvider(dir string, cfg gabitotl.VCSConfig, da *gabitotl.DeliveryAnswers) {
	if da.Provider != "" || da.Empty() || (cfg.Provider != "" && cfg.Provider != "git") {
		return
	}
	st, err := vcs.New(dir, cfg).Status(context.Background())
	if err != nil {
		return
	}
	if _, ok := platformNames[st.Provider]; ok || st.Provider == "azure-devops" {
		da.Provider = st.Provider
	}
}

func runVCSStatus(env Env, args []string) int {
	fs := flag.NewFlagSet("vcs status", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root (default: Git top-level of the current directory)")
	asJSON := fs.Bool("json", false, "print as JSON")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	dir, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs status: %v\n", err)
		return ExitError
	}
	repo, err := openRepo(env, dir)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs status: %v\n", err)
		return ExitError
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := repo.Status(ctx)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs status: %v\n", err)
		return ExitError
	}
	layout := gabitotl.NewLayout(dir)
	origin := gabitotl.InspectVCS(layout, gabitotl.ActiveUserID())
	c := repo.Config
	authState, authKnown := authOf(ctx, dir, c)
	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(struct {
			Status      any    `json:"status"`
			Configured  any    `json:"configured_provider"`
			Permissions any    `json:"permissions"`
			Delivery    any    `json:"delivery"`
			Files       any    `json:"files"`
			Automation  string `json:"automation"`
			IsSetUp     bool   `json:"delivery_configured"`
			Auth        any    `json:"auth"`
		}{st, c.Provider, c.Permissions, c, map[string]any{
			"shared": origin.SharedPath, "yours": origin.LocalPath, "yours_exists": origin.HasLocal, "yours_overrides": origin.Overrides,
		}, c.AutomationLevel(), c.IsConfigured(), authJSON(authState, authKnown)})
		return ExitOK
	}
	if !st.IsRepo {
		fmt.Fprintf(env.Stdout, "not a git repository: %s\n", dir)
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "root: %s\nbranch: %s\nhead: %s\nremote: %s\nprovider: %s (vcs.md: %s)\ndirty: %t\n",
		st.Root, dash(st.Branch), dash(st.Head), dash(st.Remote), st.Provider, c.Provider, st.Dirty)
	if c.IsConfigured() {
		fmt.Fprintf(env.Stdout, "delivery: configured (%s)\n", c.AutomationLevel())
	} else {
		fmt.Fprintln(env.Stdout, "delivery: not configured yet; run gabitotl vcs setup")
	}
	p := c.Permissions
	mine := func(key string) string {
		for _, k := range origin.Overrides {
			if k == key {
				return " (yours)"
			}
		}
		return ""
	}
	fmt.Fprintf(env.Stdout, "permissions: branches=%s%s commits=%s%s push=%s%s pull_requests=%s%s merge=%s%s\n",
		p.Branches, mine("branches"), p.Commits, mine("commits"), p.Push, mine("push"),
		p.PullRequests, mine("pull_requests"), p.MergePermission(), mine("merge"))
	fmt.Fprintf(env.Stdout, "merge_method: %s\ndelete_branch: %t\ndraft_prs: %t\nbase_branch: %s\npr_budget_lines: %d\nchain: %s\n",
		c.EffectiveMergeMethod(), c.DeleteBranch, c.DraftPRs, defaultWord(c.BaseBranch), c.Budget(), c.ChainStrategy())
	fmt.Fprintf(env.Stdout, "auth: %s\n", authLine(authState, authKnown))
	if c.Auth != "" {
		src := "shared"
		if origin.AuthFromLocal {
			src = "yours"
		}
		fmt.Fprintf(env.Stdout, "auth_ref: %s (%s)\n", c.Auth, src)
	}
	yours := "-"
	if origin.LocalPath != "" {
		yours = origin.LocalPath
		if !origin.HasLocal {
			yours += " (not created)"
		}
	}
	fmt.Fprintf(env.Stdout, "files: shared=%s yours=%s\n", origin.SharedPath, yours)
	if len(origin.Overrides) > 0 {
		fmt.Fprintf(env.Stdout, "overridden by you: %s\n", strings.Join(origin.Overrides, ", "))
	}
	return ExitOK
}

// authLine is the `auth:` value of vcs status: ok, or what is missing with the
// exact fix, or n/a for a platform GabitoTL does not drive.
func authLine(st vcs.AuthState, known bool) string {
	switch {
	case !known:
		return "n/a"
	case st.OK():
		return "ok (" + st.Provider + ")"
	}
	return strings.ReplaceAll(st.Status, "-", " ") + "; " + st.Fix
}

func authJSON(st vcs.AuthState, known bool) map[string]string {
	if !known {
		return map[string]string{"status": "n/a"}
	}
	return map[string]string{"provider": st.Provider, "status": st.Status, "fix": st.Fix}
}

func defaultWord(s string) string {
	if s == "" {
		return "(repository default)"
	}
	return s
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func runVCSBranch(env Env, args []string) int {
	fs := flag.NewFlagSet("vcs branch", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root (default: Git top-level of the current directory)")
	task := fs.String("task", "", "task id; the branch is feature/<task> (required)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	if *task == "" {
		fmt.Fprintln(env.Stderr, "Usage: gabitotl vcs branch --task ID [--root DIR]")
		return ExitUsage
	}
	dir, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs branch: %v\n", err)
		return ExitError
	}
	repo, err := openRepo(env, dir)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs branch: %v\n", err)
		return ExitError
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	name, err := repo.CreateTaskBranch(ctx, *task)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs branch: %v\n", err)
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "switched to new branch %s\n", name)
	return ExitOK
}
