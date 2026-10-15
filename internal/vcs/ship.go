package vcs

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// ErrNothingToShip means there is no change and no commit ahead of the base.
var ErrNothingToShip = errors.New("vcs: nothing to ship: no changes and no commits ahead of the base branch")

// Why a ship stopped after the pull request without merging. These are planned
// outcomes, not failures.
const (
	StopMergeDenied = "merge-denied"
	StopMergeAsk    = "merge-ask"
	StopMergeNo     = "merge-declined"
)

// Step is one thing ship did (or, in a dry run, would do). Code is a stable
// identifier the presenter words; Args carry the values.
type Step struct {
	Code string            `json:"code"`
	Args map[string]string `json:"args,omitempty"`
}

// ShipPR is a pull request opened by ship.
type ShipPR struct {
	Branch string `json:"branch"`
	Base   string `json:"base"`
	URL    string `json:"url,omitempty"`
	Lines  int    `json:"lines"`
	Merged bool   `json:"merged"`
}

// ShipResult is the machine-readable outcome of a ship.
type ShipResult struct {
	DryRun bool     `json:"dry_run"`
	Steps  []Step   `json:"steps"`
	Branch string   `json:"branch"`
	Base   string   `json:"base"`
	Lines  int      `json:"lines"`
	PRs    []ShipPR `json:"prs"`
	Merged bool     `json:"merged"`
	Reason string   `json:"reason,omitempty"`
	Error  string   `json:"error,omitempty"`
}

// ShipOptions are the inputs of Repo.Ship.
type ShipOptions struct {
	Message string
	Title   string
	Body    string
	DryRun  bool
	// Host is required unless DryRun, which never touches the platform.
	Host   Host
	Wait   time.Duration
	Waiter Waiter
	// OnStep is told each step as it happens; may be nil.
	OnStep func(Step)
}

type shipCommit struct {
	sha   string
	lines int
}

// generatedNames are files whose lines are never authored by hand.
var generatedNames = map[string]bool{
	"go.sum": true, "go.work.sum": true, "package-lock.json": true, "yarn.lock": true,
	"pnpm-lock.yaml": true, "cargo.lock": true, "poetry.lock": true, "gemfile.lock": true,
	"composer.lock": true, "pipfile.lock": true,
}

// IsGenerated reports whether a path is a lock file, a snapshot, a minified or
// generated file or vendored code, so its lines do not count as authored.
func IsGenerated(p string) bool {
	if i := strings.Index(p, " => "); i >= 0 { // rename: count by the new name
		p = strings.TrimSuffix(p[i+4:], "}")
	}
	p = strings.ToLower(path.Clean(strings.ReplaceAll(p, "\\", "/")))
	if generatedNames[path.Base(p)] {
		return true
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "vendor" || seg == "node_modules" {
			return true
		}
	}
	for _, suf := range []string{".min.js", ".min.css", ".snap", ".pb.go", "_generated.go"} {
		if strings.HasSuffix(p, suf) {
			return true
		}
	}
	return strings.Contains(path.Base(p), ".generated.")
}

// countNumstat adds up the authored lines (additions plus deletions) of
// `git ... --numstat` output, skipping binary and generated files.
func countNumstat(out string) int {
	total := 0
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) != 3 || IsGenerated(f[2]) {
			continue
		}
		a, errA := strconv.Atoi(f[0])
		d, errD := strconv.Atoi(f[1])
		if errA == nil && errD == nil {
			total += a + d
		}
	}
	return total
}

// sliceCommits splits commits into consecutive slices of at most budget lines,
// greedily and only at commit boundaries. A commit over the budget is a slice of
// its own (never split); oversized lists those commits.
func sliceCommits(commits []shipCommit, budget int) (slices [][]shipCommit, oversized []shipCommit) {
	var cur []shipCommit
	curLines := 0
	flush := func() {
		if len(cur) > 0 {
			slices = append(slices, cur)
		}
		cur, curLines = nil, 0
	}
	for _, c := range commits {
		if len(cur) > 0 && curLines+c.lines > budget {
			flush()
		}
		cur, curLines = append(cur, c), curLines+c.lines
		if c.lines > budget {
			oversized = append(oversized, c)
			flush()
		}
	}
	flush()
	return slices, oversized
}

// commitsAhead lists the commits of branch not in baseRef, oldest first, with
// their authored line counts.
func (r *Repo) commitsAhead(ctx context.Context, baseRef, branch string) ([]shipCommit, error) {
	out, err := r.Git(ctx, "rev-list", "--reverse", baseRef+".."+branch)
	if err != nil {
		return nil, err
	}
	var cs []shipCommit
	for _, sha := range strings.Fields(out) {
		stat, err := r.Git(ctx, "show", "--numstat", "--format=", sha)
		if err != nil {
			return nil, err
		}
		cs = append(cs, shipCommit{sha: sha, lines: countNumstat(stat)})
	}
	return cs, nil
}

// dirty reports uncommitted work, ignoring .gabitotl/local, which is never committed.
func (r *Repo) dirty(ctx context.Context) (bool, error) {
	out, err := r.Git(ctx, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 3 && !inLocalDir(strings.Trim(line[3:], "\"")) {
			return true, nil
		}
	}
	return false, nil
}

func taskSlug(title string) string {
	s := gabitotl.Slug(title)
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		s = "change"
	}
	return s
}

// Ship runs the whole delivery from the saved settings: commit when dirty, push,
// open (or reuse) the pull request(s), merge when permitted and the checks pass,
// and come back to an up-to-date base. Work above pr_budget_lines is sliced at
// commit boundaries into stacked pull requests when chain is "stacked"; size is
// never a question. It never pushes the base branch. With DryRun nothing is
// changed and the platform is not contacted.
func (r *Repo) Ship(ctx context.Context, o ShipOptions) (res ShipResult, err error) {
	res.DryRun = o.DryRun
	emit := func(code string, kv ...string) {
		s := Step{Code: code}
		if len(kv) > 0 {
			s.Args = map[string]string{}
			for i := 0; i+1 < len(kv); i += 2 {
				s.Args[kv[i]] = kv[i+1]
			}
		}
		res.Steps = append(res.Steps, s)
		if o.OnStep != nil {
			o.OnStep(s)
		}
	}
	defer func() {
		if err != nil {
			res.Error = err.Error()
		}
	}()
	if strings.TrimSpace(o.Title) == "" {
		return res, errors.New("vcs: pull request title is required")
	}
	if !o.DryRun && o.Host == nil {
		return res, errors.New("vcs: ship needs a platform host")
	}
	if remote, _ := r.Git(ctx, "remote", "get-url", "origin"); remote == "" {
		return res, ErrNoRemote
	}
	base, err := r.BaseBranch(ctx)
	if err != nil {
		return res, err
	}
	cur, err := r.CurrentBranch(ctx)
	if err != nil {
		return res, err
	}
	res.Base, res.Branch = base, cur
	emit("login")
	baseRef := base
	if _, e := r.Git(ctx, "rev-parse", "--verify", "-q", "refs/remotes/origin/"+base); e == nil {
		baseRef = "origin/" + base
	}
	isDirty, err := r.dirty(ctx)
	if err != nil {
		return res, err
	}
	ahead, err := r.Git(ctx, "rev-list", "--count", baseRef+"..HEAD")
	if err != nil {
		return res, err
	}
	if !isDirty && ahead == "0" {
		return res, ErrNothingToShip
	}
	if isDirty && strings.TrimSpace(o.Message) == "" {
		return res, ErrInvalidCommitMessage
	}

	// Never push the base branch: work that sits on it moves to a task branch
	// created at HEAD, and the local base is left exactly as it was.
	branch := cur
	if cur == base {
		slug := taskSlug(o.Title)
		name, nerr := r.TaskBranchName(slug)
		if nerr != nil {
			return res, nerr
		}
		if _, e := r.Git(ctx, "rev-parse", "--verify", "-q", "refs/heads/"+name); e == nil {
			head, _ := r.Git(ctx, "rev-parse", "--short", "HEAD")
			name += "-" + head
		}
		branch = name
		emit("branch", "branch", branch, "base", base)
		if !o.DryRun {
			if err = Gate(r.Config.Permissions, OpBranch, "create branch "+branch, r.Confirm); err != nil {
				return res, err
			}
			if _, err = r.Git(ctx, "switch", "-c", branch); err != nil {
				return res, err
			}
			emit("base-untouched", "base", base)
		}
		res.Branch = branch
	}
	if isDirty {
		if o.DryRun {
			emit("commit", "message", firstLine(o.Message))
		} else {
			hash, cerr := r.CommitWork(ctx, o.Message, nil)
			if cerr != nil && !errors.Is(cerr, ErrNothingToCommit) {
				return res, cerr
			}
			if cerr == nil {
				emit("commit", "hash", short(hash))
			}
		}
	}

	// Size: the commits ahead of the base, plus (dry run only) the pending work.
	commits, err := r.commitsAhead(ctx, baseRef, "HEAD")
	if err != nil {
		return res, err
	}
	if o.DryRun && isDirty {
		stat, _ := r.Git(ctx, "diff", "HEAD", "--numstat")
		commits = append(commits, shipCommit{lines: countNumstat(stat)})
	}
	for _, c := range commits {
		res.Lines += c.lines
	}
	budget := r.Config.Budget()
	slices := [][]shipCommit{commits}
	var oversized []shipCommit
	if res.Lines > budget && r.Config.ChainStrategy() == gabitotl.ChainStacked {
		slices, oversized = sliceCommits(commits, budget)
	}
	emit("size", "lines", strconv.Itoa(res.Lines), "budget", strconv.Itoa(budget), "commits", strconv.Itoa(len(commits)))
	chain := len(slices) > 1
	if chain {
		emit("slices", "count", strconv.Itoa(len(slices)))
	}
	for _, c := range oversized {
		emit("oversized", "commit", short(c.sha), "lines", strconv.Itoa(c.lines))
	}

	// Who may merge, decided before anything is published.
	stop := ""
	perm := r.Config.Permissions.MergePermission()
	switch {
	case perm == gabitotl.PermDeny:
		stop = StopMergeDenied
	case perm == gabitotl.PermAsk && (o.DryRun || r.Confirm == nil):
		stop = StopMergeAsk
	}

	// Branches and pull requests.
	type target struct {
		branch, base string
		lines        int
		pr           PR
		published    bool
	}
	var targets []target
	if !chain {
		targets = []target{{branch: branch, base: base, lines: res.Lines}}
	} else {
		prev := base
		for k, s := range slices {
			name := fmt.Sprintf("%s-%d", branch, k+1)
			l := 0
			for _, c := range s {
				l += c.lines
			}
			targets = append(targets, target{branch: name, base: prev, lines: l})
			prev = name
		}
	}
	if o.DryRun {
		for _, t := range targets {
			emit("push", "branch", t.branch)
			emit("pr", "branch", t.branch, "base", t.base)
			res.PRs = append(res.PRs, ShipPR{Branch: t.branch, Base: t.base, Lines: t.lines})
		}
		return res, shipDecision(&res, emit, stop, chain, r)
	}
	if chain {
		for k, s := range slices {
			end := s[len(s)-1].sha
			name := targets[k].branch
			if have, e := r.Git(ctx, "rev-parse", "--verify", "-q", "refs/heads/"+name); e == nil {
				if have != end {
					return res, fmt.Errorf("vcs: branch %s already exists at a different commit; delete it and run ship again", name)
				}
			} else {
				if err = Gate(r.Config.Permissions, OpBranch, "create branch "+name, r.Confirm); err != nil {
					return res, err
				}
				if _, err = r.Git(ctx, "branch", name, end); err != nil {
					return res, err
				}
			}
		}
	}
	draft := r.Config.DraftPRs
	for k := range targets {
		t := &targets[k]
		if _, err = r.PushTask(ctx, t.branch); err != nil {
			return res, err
		}
		emit("push", "branch", t.branch)
		title, body := o.Title, o.Body
		if chain {
			title = fmt.Sprintf("%s (%d/%d)", o.Title, k+1, len(targets))
			body = strings.TrimSpace(body + fmt.Sprintf("\n\nPart %d of %d of a stacked series; each part targets the previous one.", k+1, len(targets)))
		}
		t.pr, err = r.OpenPR(ctx, o.Host, PRRequest{Title: title, Body: body, Base: t.base, Head: t.branch, Draft: draft})
		if err != nil {
			return res, err
		}
		t.published = r.IsPublished(ctx, t.branch)
		emit("pr", "branch", t.branch, "base", t.base, "url", t.pr.URL)
		res.PRs = append(res.PRs, ShipPR{Branch: t.branch, Base: t.base, URL: t.pr.URL, Lines: t.lines})
	}

	// The merge question is asked once, for the whole series.
	granted := *r
	granted.Config.Permissions.Merge = gabitotl.PermAllow
	if stop == "" && perm == gabitotl.PermAsk {
		if gerr := Gate(r.Config.Permissions, OpMerge, fmt.Sprintf("merge %d pull request(s)", len(targets)), r.Confirm); gerr != nil {
			stop = StopMergeNo
		}
	}
	if stop != "" {
		return res, shipDecision(&res, emit, stop, chain, r)
	}
	if chain {
		granted.Config.MergeMethod = gabitotl.MergeMerge
		if r.Config.EffectiveMergeMethod() != gabitotl.MergeMerge {
			emit("chain-note", "configured", r.Config.EffectiveMergeMethod())
		}
	}
	opts := granted.MergeDefaults()
	for k, t := range targets {
		if _, err = o.Waiter.WaitForChecks(ctx, o.Host, r.Dir, t.pr, o.Wait); err != nil {
			return res, err
		}
		if k+1 < len(targets) { // the child leaves the parent branch before the parent is merged
			if err = granted.RetargetPR(ctx, o.Host, targets[k+1].pr, base); err != nil {
				return res, err
			}
			res.PRs[k+1].Base = base
			emit("retarget", "url", targets[k+1].pr.URL, "base", base)
		}
		if err = granted.MergePR(ctx, o.Host, t.pr, opts); err != nil {
			return res, err
		}
		res.PRs[k].Merged = true
		emit("merge", "url", t.pr.URL, "method", opts.Method)
	}
	res.Merged = true
	// Local cleanup. The original branch is only left alone when it was sliced.
	notes := r.AfterMerge(ctx, branch, !chain && targets[0].published)
	if chain {
		for _, t := range targets {
			notes = append(notes, r.AfterMerge(ctx, t.branch, t.published)...)
		}
	}
	for _, n := range notes {
		emit("note-"+n.Code, flatten(n.Args)...)
	}
	return res, nil
}

func flatten(m map[string]string) []string {
	var kv []string
	for k, v := range m {
		kv = append(kv, k, v)
	}
	return kv
}

// shipDecision records the planned stop (or, in a dry run, the merge that would
// follow) and sets the reason.
func shipDecision(res *ShipResult, emit func(string, ...string), stop string, chain bool, r *Repo) error {
	if stop != "" {
		res.Reason = stop
		emit("stop", "reason", stop)
		return nil
	}
	if res.DryRun {
		method := r.Config.EffectiveMergeMethod()
		if chain {
			method = gabitotl.MergeMerge
		}
		emit("merge", "method", method)
	}
	return nil
}
