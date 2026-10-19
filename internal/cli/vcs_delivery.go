package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/installer/ui"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

// Composition-root hooks for the delivery commands, replaced by tests so no real
// platform, clock or sleep is ever involved.
var (
	deliveryHostFn   = hostFor
	deliveryWaiterFn = func() vcs.Waiter { return vcs.Waiter{} }
)

// defaultMergeWait is how long `vcs merge` waits for the checks.
const defaultMergeWait = 20 * time.Minute

// delivery is what the commit, push, pr and merge commands share: the project
// root, the Repo with the saved settings and the Spanish wording.
type delivery struct {
	env  Env
	m    ui.Messages
	name string // "vcs commit", used in messages
	dir  string
	repo *vcs.Repo
}

// openDelivery resolves the project and its settings. On failure it has already
// printed why and returns the exit code.
func openDelivery(env Env, name, root string) (*delivery, int) {
	dir, err := resolveRoot(env, root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl %s: %v\n", name, err)
		return nil, ExitError
	}
	repo, err := openRepo(env, dir)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl %s: %v\n", name, err)
		return nil, ExitError
	}
	d := &delivery{env: env, m: ui.Active(""), name: name, dir: dir, repo: repo}
	if env.IsTTY {
		p := ui.NewPrompter(env.Stdin, env.Stderr, d.m)
		repo.Confirm = func(op, detail string) bool {
			return p.Confirm(d.m.Format("deliver.confirm."+op, "detail", detail))
		}
	}
	return d, 0
}

func (d *delivery) say(key string, pairs ...string) {
	fmt.Fprintf(d.env.Stdout, "%s %s\n", ui.SymOK, d.m.Format(key, pairs...))
}

func (d *delivery) warn(key string, pairs ...string) {
	fmt.Fprintf(d.env.Stderr, "%s %s\n", ui.SymError, d.m.Format(key, pairs...))
}

// host builds the platform host and requires a login. The fix is printed, never
// prompted for.
func (d *delivery) host(ctx context.Context) (vcs.Host, int) {
	provider := platformOf(ctx, d.dir, d.repo.Config)
	if _, ok := platformNames[provider]; !ok {
		d.warn("vcs.login.noplatform", "provider", provider)
		return nil, ExitError
	}
	h, err := deliveryHostFn(provider, d.dir, d.repo.Config)
	if err != nil {
		return nil, d.fail(err)
	}
	if err := vcs.EnsureLogin(ctx, h, d.dir); err != nil {
		return nil, d.fail(err)
	}
	return h, 0
}

// fail words an error with the next command to run and returns the exit code.
func (d *delivery) fail(err error) int {
	var perm *vcs.PermissionError
	var checks *vcs.ChecksError
	var login *vcs.LoginError
	switch {
	case errors.As(err, &login):
		d.loginFix(login.State)
	case errors.As(err, &checks):
		d.warn("deliver.err.checks."+checks.State, "detail", checks.Detail, "wait", checks.Waited.Round(time.Second).String())
	case errors.As(err, &perm):
		op := d.m.Format("deliver.op." + perm.Op)
		switch {
		case errors.Is(perm.Err, vcs.ErrDenied):
			d.warn("deliver.err.denied", "op", op)
		case d.env.IsTTY:
			d.warn("deliver.err.declined", "op", op)
		default:
			d.warn("deliver.err.ask", "op", op)
		}
	case errors.Is(err, vcs.ErrOnBaseBranch):
		base, _ := d.repo.BaseBranch(context.Background())
		d.warn("deliver.err.onbase", "base", base)
	case errors.Is(err, vcs.ErrNoBase):
		d.warn("deliver.err.nobase")
	case errors.Is(err, vcs.ErrDetached):
		d.warn("deliver.err.detached")
	case errors.Is(err, vcs.ErrNoRemote):
		d.warn("deliver.err.noremote")
	case errors.Is(err, vcs.ErrNothingToCommit):
		d.warn("deliver.commit.nothing")
	case errors.Is(err, vcs.ErrLocalPath):
		d.warn("deliver.commit.local")
	case errors.Is(err, vcs.ErrAttribution):
		d.warn("deliver.commit.attribution")
	case errors.Is(err, vcs.ErrInvalidCommitMessage):
		d.warn("deliver.commit.invalid")
	case errors.Is(err, vcs.ErrNothingToShip):
		d.warn("deliver.err.nothing")
	case errors.Is(err, vcs.ErrNoPR):
		cur, _ := d.repo.CurrentBranch(context.Background())
		d.warn("deliver.err.nopr", "branch", cur)
	default:
		fmt.Fprintf(d.env.Stderr, "gabitotl %s: %v\n", d.name, err)
	}
	return ExitError
}

// loginFix prints the exact fix for a missing login.
func (d *delivery) loginFix(st vcs.AuthState) {
	name := platformNames[st.Provider]
	switch {
	case st.Status == vcs.AuthToolMissing:
		d.warn("vcs.login.toolmissing", "tool", platformTool[st.Provider], "url", st.InstallURL)
	case st.Status == vcs.AuthUnsupported:
		d.warn("vcs.login.unsupported", "platform", name)
	case st.Command != "":
		d.warn("deliver.err.login.cmd", "platform", name, "command", st.Command)
	default:
		d.warn("deliver.err.login", "platform", name)
	}
}

func shortHash(full string) string {
	if len(full) > 8 {
		return full[:8]
	}
	return full
}

// runVCSCommit stages the given paths, or every change, and commits them. It
// never stages .gabitotl/local and never commits on the base branch.
func runVCSCommit(env Env, args []string) int {
	fl := flag.NewFlagSet("vcs commit", flag.ContinueOnError)
	fl.SetOutput(env.Stderr)
	root := fl.String("root", "", "project root (default: Git top-level of the current directory)")
	msg := fl.String("m", "", "commit message, a Conventional Commit (required)")
	paths := fl.String("paths", "", "comma-separated paths to commit (default: every change)")
	if err := fl.Parse(args); err != nil {
		return flagExit(err)
	}
	d, code := openDelivery(env, "vcs commit", *root)
	if d == nil {
		return code
	}
	if strings.TrimSpace(*msg) == "" {
		d.warn("deliver.commit.empty")
		return ExitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	hash, err := d.repo.CommitWork(ctx, *msg, splitList(*paths))
	if err != nil {
		return d.fail(err)
	}
	d.say("deliver.commit.done", "hash", shortHash(hash))
	return ExitOK
}

// runVCSPush publishes a branch to origin, never forced and never the base branch.
func runVCSPush(env Env, args []string) int {
	fl := flag.NewFlagSet("vcs push", flag.ContinueOnError)
	fl.SetOutput(env.Stderr)
	root := fl.String("root", "", "project root (default: Git top-level of the current directory)")
	branch := fl.String("branch", "", "branch to push (default: the current one)")
	if err := fl.Parse(args); err != nil {
		return flagExit(err)
	}
	d, code := openDelivery(env, "vcs push", *root)
	if d == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	pushed, err := d.repo.PushTask(ctx, *branch)
	if err != nil {
		return d.fail(err)
	}
	d.say("deliver.push.done", "branch", pushed)
	return ExitOK
}

// runVCSPR opens the pull request of the current branch, or returns the one that
// is already open.
func runVCSPR(env Env, args []string) int {
	var draft *bool
	fl := flag.NewFlagSet("vcs pr", flag.ContinueOnError)
	fl.SetOutput(env.Stderr)
	root := fl.String("root", "", "project root (default: Git top-level of the current directory)")
	title := fl.String("title", "", "pull request title (required)")
	body := fl.String("body", "", "pull request description")
	base := fl.String("base", "", "target branch (default: base_branch in vcs.md, else the repository default)")
	fl.Var(boolPtr{&draft}, "draft", "open as a draft (default: draft_prs in vcs.md)")
	if err := fl.Parse(args); err != nil {
		return flagExit(err)
	}
	d, code := openDelivery(env, "vcs pr", *root)
	if d == nil {
		return code
	}
	if strings.TrimSpace(*title) == "" {
		d.warn("deliver.pr.notitle")
		return ExitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	h, code := d.host(ctx)
	if h == nil {
		return code
	}
	req, err := d.repo.PRFor(ctx, *title, *body, *base, draft)
	if err != nil {
		return d.fail(err)
	}
	pr, err := d.repo.OpenPR(ctx, h, req)
	if err != nil {
		return d.fail(err)
	}
	d.say("deliver.pr.done", "url", pr.URL)
	return ExitOK
}

// parsePRRef reads --pr: a number is a platform id, anything else a URL.
func parsePRRef(s string) vcs.PR {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return vcs.PR{ID: s, Number: n}
	}
	return vcs.PR{URL: s}
}

// runVCSMerge waits for the checks of a pull request and merges it with the
// saved method, but only when they pass (or there are none).
func runVCSMerge(env Env, args []string) int {
	fl := flag.NewFlagSet("vcs merge", flag.ContinueOnError)
	fl.SetOutput(env.Stderr)
	root := fl.String("root", "", "project root (default: Git top-level of the current directory)")
	prRef := fl.String("pr", "", "pull request URL or number (default: the open PR of the current branch)")
	wait := fl.Duration("wait", defaultMergeWait, "how long to wait for the checks (0 reads them once)")
	if err := fl.Parse(args); err != nil {
		return flagExit(err)
	}
	d, code := openDelivery(env, "vcs merge", *root)
	if d == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), *wait+10*time.Minute)
	defer cancel()
	h, code := d.host(ctx)
	if h == nil {
		return code
	}
	var pr vcs.PR
	branch := "" // set when the PR is the one of the current branch: local cleanup applies
	if *prRef != "" {
		pr = parsePRRef(*prRef)
	} else {
		cur, err := d.repo.CurrentBranch(ctx)
		if err != nil {
			return d.fail(err)
		}
		if pr, err = d.repo.FindOpenPR(ctx, h, cur); err != nil {
			return d.fail(err)
		}
		branch = cur
	}
	published := branch != "" && d.repo.IsPublished(ctx, branch)
	d.say("deliver.merge.waiting", "url", pr.URL, "wait", wait.String())
	c, err := d.repo.MergeWhenGreen(ctx, h, deliveryWaiterFn(), pr, *wait)
	if err != nil {
		return d.fail(err)
	}
	if c.State == vcs.ChecksNone {
		d.say("deliver.merge.nochecks")
	}
	d.say("deliver.merge.done", "url", pr.URL, "method", d.repo.Config.EffectiveMergeMethod())
	for _, n := range d.repo.AfterMerge(ctx, branch, published) {
		pairs := []string{}
		for k, v := range n.Args {
			pairs = append(pairs, k, v)
		}
		d.say("deliver.note."+n.Code, pairs...)
	}
	return ExitOK
}
