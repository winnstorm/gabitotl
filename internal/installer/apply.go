package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/identity"
	"github.com/winnstorm/gabitotl/internal/importer"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/persona"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// Errors of the apply phases.
var (
	ErrVerifyFailed = errors.New("verification failed")
	ErrNotVerified  = errors.New("refusing to retire: critical verification has not passed")
)

// Check is one verification line. Group is "memory", "project", "runtime" or
// "post-retire".
type Check struct {
	Group  string
	Name   string
	OK     bool
	Detail string
}

// Failed returns the failing checks.
func Failed(cs []Check) []Check {
	var out []Check
	for _, c := range cs {
		if !c.OK {
			out = append(out, c)
		}
	}
	return out
}

// RetirePreview is what the user is asked to confirm after verification.
type RetirePreview struct {
	Args    []string // gentle-ai uninstall arguments
	Covered bool     // true when --all is used
}

// Hooks let a presenter take decisions that can only be asked once everything
// is verified. Nil hooks decline.
type Hooks struct {
	// ConfirmRetire is asked after every critical check passed and Gentle-AI is
	// present but --remove-gentle-ai was not given. Returning true retires it.
	ConfirmRetire func(RetirePreview) bool
}

// Retired records what the retirement step did.
type Retired struct {
	Personas   []rt.Action // managed persona blocks removed by adapters
	GentleArgs []string    // arguments of gentle-ai's own uninstall, nil when not run
}

// Result is everything Run did, for presenters and tests.
type Result struct {
	DryRun         bool
	Skipped        bool // nothing to apply (idempotent re-run)
	Phases         []Phase
	Snapshot       *Snapshot
	Applied        []string // runtimes configured, in order
	Runtime        map[string]rt.Result
	Imports        []importer.Outcome
	Checks         []Check
	Retired        Retired
	RolledBack     bool
	RolledBackWith []string // files restored by the rollback
	Warnings       []string
	ReportPath     string
}

type run struct {
	env     Env
	inv     Inventory
	pl      Plan
	obs     Observer
	hooks   Hooks
	res     *Result
	snap    *Snapshot
	log     *os.File
	applied []string
}

func (r *run) phase(p Phase) {
	r.res.Phases = append(r.res.Phases, p)
	r.emit(p, "start", string(p), "")
}

func (r *run) emit(p Phase, status, step, detail string) {
	e := Event{Phase: p, Status: status, Step: step, Detail: detail}
	if r.obs != nil {
		r.obs(e)
	}
	if r.log != nil { // phases, ids and counts only: never content or environment values
		fmt.Fprintf(r.log, "%s %s %s %s %s\n", r.env.Clock().Format(time.RFC3339), p, status, step, detail)
	}
}

func (r *run) warn(format string, a ...any) {
	r.res.Warnings = append(r.res.Warnings, fmt.Sprintf(format, a...))
}

func (r *run) ropts() rt.Options {
	return rt.Options{Scope: rt.ScopeGlobal, PersonaText: persona.DefaultText(), Memory: r.pl.Memory, MigrateGentleman: r.pl.Options.MigratePersona, Version: r.env.Version}
}

// Run executes the plan. Order is fixed and enforced here:
//
//	plan -> snapshot -> migrate knowledge -> configure runtimes -> verify memory
//	-> verify project -> retire -> report
//
// Nothing mutates before the snapshot exists (a); retirement only runs after
// every critical check passed (b). Any failure after mutations start reverts the
// GabitoTL changes from the ownership state and the snapshot, leaving
// Gentle-AI and Engram intact.
func Run(ctx context.Context, env Env, inv Inventory, pl Plan, obs Observer, hooks Hooks) (Result, error) {
	res := Result{Runtime: map[string]rt.Result{}}
	r := &run{env: env, inv: inv, pl: pl, obs: obs, hooks: hooks, res: &res}
	r.phase(PhasePlan)
	if pl.Options.DryRun {
		res.DryRun = true
		return res, nil
	}
	if !pl.Mutates() {
		res.Skipped = true
		return res, nil
	}
	if pl.HasNote(NoteMemoryUnhealthy) {
		return res, ErrMemoryUnhealthy
	}

	r.phase(PhaseSnapshot)
	snap, err := CreateSnapshot(ctx, env, inv, pl, obs)
	res.Snapshot = snap
	if err != nil {
		return res, fmt.Errorf("snapshot: %w", err) // nothing was changed
	}
	r.snap = snap
	if f, err := os.OpenFile(filepath.Join(env.StateDir(), "logs", snap.ID+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		r.log = f
		defer f.Close()
	}

	if err := r.mutate(ctx); err != nil {
		var partial *PartialError
		if errors.As(err, &partial) { // after Gentle-AI's own uninstall: keep GabitoTL, point to the snapshot
			r.snap.Manifest.Status = "partial"
			_ = r.snap.Save(r.env)
			r.warn("GabitoTL stays configured. Gentle-AI may be partially removed: `gabitotl restore %s` returns the saved configuration files", r.snap.ID)
			if rerr := r.writeReport(ctx, "partial"); rerr != nil {
				r.warn("report: %v", rerr)
			}
			return res, err
		}
		r.rollback(err)
		return res, err
	}
	return res, nil
}

// mutate runs every phase after the snapshot. An error means "revert".
func (r *run) mutate(ctx context.Context) error {
	if err := r.migrateKnowledge(ctx); err != nil {
		return fmt.Errorf("%s: %w", PhaseMigrate, err)
	}
	if err := r.configure(ctx); err != nil {
		return fmt.Errorf("%s: %w", PhaseConfigure, err)
	}
	r.phase(PhaseVerifyMemory)
	r.res.Checks = append(r.res.Checks, r.verifyMemory(ctx, "memory")...)
	r.phase(PhaseVerifyProject)
	r.res.Checks = append(r.res.Checks, r.verifyProject()...)
	r.res.Checks = append(r.res.Checks, r.verifyRuntimes("runtime")...)
	if bad := Failed(r.res.Checks); len(bad) > 0 {
		return fmt.Errorf("%w: %s", ErrVerifyFailed, bad[0].Name+": "+bad[0].Detail)
	}
	if err := r.retire(ctx); err != nil {
		return err
	}
	return r.finish(ctx)
}

// projectProvider is the VCS provider written to vcs.md.
func projectProvider(inv Inventory) string {
	switch {
	case slices.Contains(gabitotl.VCSProviders, inv.Project.Provider):
		return inv.Project.Provider
	case inv.Project.GitRepo:
		return "git"
	}
	return "local"
}

func (r *run) migrateKnowledge(ctx context.Context) error {
	r.phase(PhaseMigrate)
	if r.env.ProjectRoot == "" {
		r.warn("no project root: only the global configuration was set up")
		return nil
	}
	mode := r.pl.Options.Mode
	if mode == "" && r.pl.MemoryChoice == gabitotl.MemoryMemoryMCP {
		mode = gabitotl.ModeTeam
	}
	l := gabitotl.NewLayout(r.env.ProjectRoot)
	res, err := gabitotl.Init(r.env.ProjectRoot, gabitotl.InitOptions{
		Mode: mode, Provider: r.pl.MemoryChoice, VCSProvider: projectProvider(r.inv),
	})
	if err != nil {
		return err
	}
	r.emit(PhaseMigrate, "ok", "create .gabitotl", fmt.Sprintf("%d file(s)", len(res.Created)))
	if r.pl.Options.AutomateGit && slices.Contains(res.Created, relTo(r.env.ProjectRoot, l.VCS())) {
		cfg, err := gabitotl.LoadVCS(l)
		if err != nil {
			return err
		}
		cfg.RemoteActions = "automatic"
		cfg.Permissions = gabitotl.VCSPermissions{Branches: gabitotl.PermAllow, Commits: gabitotl.PermAllow, Push: gabitotl.PermAllow, PullRequests: gabitotl.PermAllow}
		if err := gabitotl.SaveVCS(l, cfg); err != nil {
			return err
		}
	}
	if r.pl.Options.UserName != "" {
		u, err := identity.New(r.pl.Options.User, r.pl.Options.UserName, "")
		if err != nil {
			return err
		}
		if err := identity.Save(r.env.ProjectRoot, u); err != nil {
			return err
		}
	}
	if len(r.pl.RuntimeOrder) > 0 {
		if cur, err := gabitotl.LoadRuntime(l); err == nil && cur.Runtime == "unset" {
			if err := rt.SetRuntime(l, r.pl.RuntimeOrder[0], "", false); err != nil {
				return err
			}
		}
	}

	// Import knowledge: read other harnesses, synthesize, record in imports.md.
	opts := importer.Options{}
	if cfg, err := gabitotl.LoadMemory(l); err == nil && r.env.OpenMemory != nil {
		if p, err := r.env.OpenMemory(cfg); err != nil {
			r.warn("memory unavailable (%v): imports recorded in imports.md only", err)
		} else {
			opts.Sink = KnowledgeSink(p, cfg)
		}
	}
	outs, err := importer.Run(ctx, l, importerEnv(r.env, r.inv.Project.Key), opts)
	r.res.Imports = outs
	for _, o := range outs {
		if o.Err != "" {
			r.warn("import %s: %s", o.Source, o.Err)
		}
	}
	return err
}

func relTo(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil {
		return rel
	}
	return p
}

// configure applies the install-time actions of every selected runtime.
// REMOVE-AFTER-VERIFY actions are not part of it (invariant b).
func (r *run) configure(ctx context.Context) error {
	r.phase(PhaseConfigure)
	renv := r.env.RT()
	for _, name := range r.pl.RuntimeOrder {
		a, _ := r.env.Runtimes.Get(name)
		r.applied = append(r.applied, name) // before Apply: a partial apply is reverted too
		res, err := a.Apply(renv, r.pl.Install[name])
		r.res.Runtime[name] = res
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		r.res.Applied = append(r.res.Applied, name)
		r.emit(PhaseConfigure, "ok", "configure "+name, fmt.Sprintf("%d change(s)", len(res.Applied)))
	}
	return ctx.Err()
}

// verifyMemory runs engram doctor again (the "after" of before/after) and
// checks that memory.md names the provider the plan chose.
func (r *run) verifyMemory(ctx context.Context, group string) []Check {
	var out []Check
	if p, _, ok := r.env.Engram(r.inv.Project.Key); ok && r.pl.MemoryChoice == gabitotl.MemoryEngram {
		rep := p.Doctor(ctx)
		out = append(out, Check{Group: group, Name: "engram doctor", OK: rep.OK(), Detail: failedMemory(rep.Checks)})
		_, err := os.Stat(r.inv.Memory.DataDir)
		out = append(out, Check{Group: group, Name: "engram data dir", OK: err == nil || !r.inv.Memory.HasDir, Detail: orStr(errText(err), "present")})
	} else if r.pl.MemoryChoice == gabitotl.MemoryEngram {
		out = append(out, Check{Group: group, Name: "engram", OK: true, Detail: "not installed; memory left unconnected"})
	}
	if r.env.ProjectRoot != "" {
		m, err := gabitotl.LoadMemory(gabitotl.NewLayout(r.env.ProjectRoot))
		out = append(out, Check{Group: group, Name: "memory.md", OK: err == nil && m.Provider == r.pl.MemoryChoice, Detail: errText(err)})
	}
	return out
}

func errText(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}

func failedMemory(cs []memory.Check) string {
	var bad []string
	for _, c := range cs {
		if c.Status == memory.StatusError {
			bad = append(bad, c.Name+": "+c.Message)
		}
	}
	return strings.Join(bad, "; ")
}

func (r *run) verifyProject() []Check {
	var out []Check
	if r.env.ProjectRoot == "" {
		return out
	}
	l := gabitotl.NewLayout(r.env.ProjectRoot)
	add := func(name string, err error) {
		out = append(out, Check{Group: "project", Name: name, OK: err == nil, Detail: errText(err)})
	}
	_, err := gabitotl.LoadProject(l)
	add("project.md", err)
	_, err = gabitotl.LoadMemory(l)
	add("memory.md", err)
	_, err = gabitotl.LoadRuntime(l)
	add("runtime.md", err)
	_, err = gabitotl.LoadVCS(l)
	add("vcs.md", err)
	_, err = gabitotl.LoadWorkflow(l)
	add("workflow.md", err)
	if r.pl.Options.UserName != "" {
		_, err = identity.Active(r.env.ProjectRoot)
		add("identity", err)
	}
	return out
}

// verifyRuntimes asks each configured adapter to re-plan: anything still
// pending is a failed check.
func (r *run) verifyRuntimes(group string) []Check {
	var out []Check
	renv := r.env.RT()
	for _, name := range r.pl.RuntimeOrder {
		a, _ := r.env.Runtimes.Get(name)
		for _, c := range a.Verify(renv, r.ropts()) {
			out = append(out, Check{Group: group, Name: name + ": " + c.Name, OK: c.OK, Detail: c.Detail})
		}
	}
	return out
}

// rollback reverts every GabitoTL mutation of this run: first through the
// ownership state (what each adapter says it wrote), then byte for byte from the
// snapshot. Gentle-AI and Engram are never part of it.
func (r *run) rollback(cause error) {
	r.emit(PhaseReport, "error", "rollback", cause.Error())
	renv := r.env.RT()
	for i := len(r.applied) - 1; i >= 0; i-- {
		a, _ := r.env.Runtimes.Get(r.applied[i])
		if _, err := a.RemoveGabitoConfig(renv); err != nil {
			r.warn("rollback: %s: %v", r.applied[i], err)
		}
	}
	files, err := r.snap.RestoreFiles(true)
	if err != nil {
		r.warn("rollback: %v", err)
	}
	r.res.RolledBack, r.res.RolledBackWith = true, files
	if len(r.res.Imports) > 0 {
		r.warn("knowledge already saved to memory stays there (memory is never deleted)")
	}
	r.snap.Manifest.Status = "rolled-back"
	_ = r.snap.Save(r.env)
	r.emit(PhaseReport, "ok", "rolled back", fmt.Sprintf("%d file(s) restored", len(files)))
}

// finish closes a successful run: the report, then the snapshot status.
func (r *run) finish(ctx context.Context) error {
	if err := r.writeReport(ctx, "complete"); err != nil {
		return fmt.Errorf("%s: %w", PhaseReport, err)
	}
	r.snap.Manifest.Status = "complete"
	return r.snap.Save(r.env)
}
