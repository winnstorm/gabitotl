package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/importer"
	"github.com/winnstorm/gabitotl/internal/persona"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// Sources of a run: a plain install or a migration from another harness.
const (
	SourceInstall  = "install"
	SourceGentleAI = "gentle-ai"
)

// KindImport is the IMPORT verb of a plan (spec 42); the other verbs are the
// runtime Kinds.
const KindImport rt.Kind = "IMPORT"

// Options are the user's decisions. Everything detectable is NOT here: runtimes,
// Engram, Git and the provider are found, not asked.
type Options struct {
	Source         string   // SourceInstall (default) or SourceGentleAI
	Runtimes       []string // empty: every detected, usable runtime
	Memory         string   // engram | memory-mcp; empty: from .gabitotl/memory.md, else engram
	Mode           string   // solo | team; empty: solo (team when Memory is memory-mcp)
	User           string   // developer id (slug)
	UserName       string   // developer display name
	AutomateGit    bool     // let Gabito handle commits and PRs (vcs.md permissions)
	MigratePersona bool     // retire the managed Gentleman/Gentle-AI persona block after verify
	RemoveGentleAI bool     // run Gentle-AI's own uninstall after verify (destructive)
	PreserveEngram bool     // informational: Engram is always adopted, never copied
	DryRun         bool
}

// Note is something presenters must tell the user about the plan.
type Note struct {
	Code   string
	Detail string
}

// Plan notes.
const (
	NoteNoRuntimes      = "no-runtimes"
	NoteRuntimeMissing  = "runtime-not-installed"
	NoteEngramMissing   = "engram-missing"
	NoteNoMigrator      = "no-memory-migrator"
	NoteGentleNotFound  = "gentle-ai-not-found"
	NotePersonaConflict = "persona-conflict"
	NoteGentleUnmatched = "gentle-ai-agents-unmatched"
	NoteGentleManual    = "gentle-ai-manual-uninstall"
	NoteMemoryUnhealthy = "memory-unhealthy"
	NoteEngramNoBackup  = "engram-not-exportable"
)

// PlanItem is one line of the plan.
type PlanItem struct {
	Kind    rt.Kind
	Target  string
	Why     string // reason code; presenters map it to wording ("exists", "snapshot", ...)
	Detail  string // raw adapter detail (English), shown when Why is empty
	Runtime string
	Count   int // IMPORT: number of knowledge items
}

// Reason codes of PlanItem.Why.
const (
	WhyExists        = "exists"
	WhySnapshot      = "snapshot"
	WhyEngramAdopted = "engram-adopted"
	WhyEngramProject = "engram-project"
	WhyGentleBackups = "gentle-backups"
	WhyUserAssets    = "user-assets"
	WhyUninstallAll  = "gentle-uninstall-all"
	WhyImportDone    = "import-unchanged"
)

// Plan is the pure description of a run. Building it writes nothing.
type Plan struct {
	Options    Options
	Items      []PlanItem
	SnapshotID string
	// Install holds the CREATE/MODIFY/NO-ACTION/PRESERVE actions per runtime; Retire
	// holds the REMOVE-AFTER-VERIFY ones. They are applied in different phases
	// (invariant b).
	Install      map[string][]rt.Action
	Retire       map[string][]rt.Action
	RuntimeOrder []string
	ProjectFiles []string // .gabitotl files Init would create (absolute paths)
	Memory       rt.MemoryMCP
	MemoryChoice string
	GentleArgs   []string // gentle-ai uninstall arguments, nil when not retiring
	Notes        []Note
}

// Sentinel errors of plan building.
var ErrUnknownRuntime = errors.New("unknown runtime")

// HasNote reports whether the plan carries a note with this code.
func (p Plan) HasNote(code string) bool {
	return slices.ContainsFunc(p.Notes, func(n Note) bool { return n.Code == code })
}

func (p *Plan) note(code, detail string) { p.Notes = append(p.Notes, Note{code, detail}) }

func (p *Plan) add(kind rt.Kind, target, why string) {
	p.Items = append(p.Items, PlanItem{Kind: kind, Target: target, Why: why})
}

// SnapshotID is the global snapshot name: <timestamp>_<source> (spec 26).
func SnapshotID(env Env, source string) string {
	return env.Clock().Format("2006-01-02_1504") + "_" + source
}

func projectFiles(l gabitotl.Layout) []string {
	return []string{l.Project(), l.Memory(), l.Runtime(), l.VCS(), l.Workflow(), l.Imports(), l.Gitignore()}
}

// importerEnv builds the read-only importer Env from the installer Env.
func importerEnv(env Env, key string) importer.Env {
	ie := importer.Env{Home: env.Home, ProjectRoot: env.ProjectRoot, ProjectKey: key, Getenv: env.Getenv, LookPath: env.LookPath, Now: env.Now}
	if p, _, ok := env.Engram(key); ok {
		ie.Projects = func(ctx context.Context) ([]importer.ProjectInfo, error) {
			ps, err := p.Projects(ctx)
			out := make([]importer.ProjectInfo, len(ps))
			for i, x := range ps {
				out[i] = importer.ProjectInfo{Name: x.Name, Observations: x.Observations}
			}
			return out, err
		}
	}
	return ie
}

// BuildPlan is pure: it detects nothing new and writes nothing. It reads
// adapter plans (themselves pure) and previews importers in dry-run mode.
func BuildPlan(ctx context.Context, env Env, inv Inventory, opts Options) (Plan, error) {
	if opts.Source == "" {
		opts.Source = SourceInstall
	}
	pl := Plan{Options: opts, Install: map[string][]rt.Action{}, Retire: map[string][]rt.Action{}}
	pl.SnapshotID = SnapshotID(env, opts.Source)
	g := inv.Other.GentleAI
	if opts.Source == SourceGentleAI && !g.Found {
		pl.note(NoteGentleNotFound, "")
	}

	// Runtimes: explicit names must exist; otherwise every usable detected one.
	selected := opts.Runtimes
	if len(selected) == 0 {
		for _, r := range inv.Runtimes {
			if r.Usable() {
				selected = append(selected, r.Name)
			}
		}
	}
	for _, n := range selected {
		if _, ok := env.Runtimes.Get(n); !ok {
			return pl, fmt.Errorf("%w %q (known: %s)", ErrUnknownRuntime, n, strings.Join(env.Runtimes.Names(), ", "))
		}
	}
	// Registry order, no duplicates, so shared files are planned once per owner.
	for _, a := range env.Runtimes.All() {
		if slices.Contains(selected, a.Name()) {
			pl.RuntimeOrder = append(pl.RuntimeOrder, a.Name())
			if r, ok := inv.Runtime(a.Name()); ok && !r.Usable() {
				pl.note(NoteRuntimeMissing, a.Name())
			}
		}
	}
	if len(pl.RuntimeOrder) == 0 {
		pl.note(NoteNoRuntimes, "")
	}

	// Memory: adopt what exists. A missing Engram is never installed here.
	pl.MemoryChoice = opts.Memory
	l := gabitotl.NewLayout(env.ProjectRoot)
	if pl.MemoryChoice == "" && inv.Project.Exists {
		if m, err := gabitotl.LoadMemory(l); err == nil {
			pl.MemoryChoice = m.Provider
		}
	}
	if pl.MemoryChoice == "" {
		pl.MemoryChoice = gabitotl.MemoryEngram
		if opts.Mode == gabitotl.ModeTeam {
			pl.MemoryChoice = gabitotl.MemoryMemoryMCP
		}
	}
	if pl.MemoryChoice != gabitotl.MemoryEngram && pl.MemoryChoice != gabitotl.MemoryMemoryMCP {
		return pl, fmt.Errorf("--memory must be engram or memory-mcp, got %q", pl.MemoryChoice)
	}
	pl.Options.Memory = pl.MemoryChoice
	switch {
	case pl.MemoryChoice == gabitotl.MemoryEngram && inv.Memory.Present():
		pl.Memory = rt.MemoryMCP{Name: "engram", Command: "engram", Args: []string{"mcp"}}
	case pl.MemoryChoice == gabitotl.MemoryEngram:
		pl.note(NoteEngramMissing, "")
	}
	if pl.MemoryChoice == gabitotl.MemoryMemoryMCP && inv.Memory.Present() {
		if _, err := env.Migrators.Lookup("engram", "memory-mcp"); err != nil {
			pl.note(NoteNoMigrator, "")
		}
	}
	if inv.Memory.DoctorRan && !inv.Memory.DoctorOK {
		pl.note(NoteMemoryUnhealthy, "")
	}
	if inv.Memory.Present() && !inv.Memory.HasBinary {
		pl.note(NoteEngramNoBackup, "")
	}

	// Snapshot dir.
	pl.add(rt.Create, filepath.Join(env.StateDir(), "migrations", pl.SnapshotID)+string(filepath.Separator), WhySnapshot)

	// Project files.
	if env.ProjectRoot != "" {
		for _, f := range projectFiles(l) {
			if _, err := os.Stat(f); err == nil {
				pl.add(rt.Preserve, f, WhyExists)
				continue
			}
			pl.ProjectFiles = append(pl.ProjectFiles, f)
			pl.add(rt.Create, f, "")
		}
	}

	// Runtime actions, split into install-time and retire-time (invariant b).
	renv := env.RT()
	ropts := rt.Options{Scope: rt.ScopeGlobal, PersonaText: persona.DefaultText(), Memory: pl.Memory, MigrateGentleman: opts.MigratePersona, Version: env.Version}
	for _, name := range pl.RuntimeOrder {
		a, _ := env.Runtimes.Get(name)
		acts, err := a.Plan(renv, ropts)
		if err != nil {
			return pl, fmt.Errorf("%s: %w", name, err)
		}
		for _, ac := range acts {
			pl.Items = append(pl.Items, PlanItem{Kind: ac.Kind, Target: ac.Path + " [" + ac.Target + "]", Detail: ac.Detail, Runtime: name})
			if ac.Kind == rt.RemoveAfterVerify {
				pl.Retire[name] = append(pl.Retire[name], ac)
			} else {
				pl.Install[name] = append(pl.Install[name], ac)
			}
			if ac.Kind == rt.Preserve && strings.Contains(ac.Detail, "persona conflict") {
				pl.note(NotePersonaConflict, name)
			}
		}
	}

	// Knowledge to import (read-only preview).
	if env.ProjectRoot != "" {
		outs, err := importer.Run(ctx, l, importerEnv(env, inv.Project.Key), importer.Options{DryRun: true})
		if err == nil {
			for _, o := range outs {
				switch o.Status {
				case importer.StatusWouldApply:
					pl.Items = append(pl.Items, PlanItem{Kind: KindImport, Target: o.Source, Count: len(o.Items)})
				case importer.StatusUnchanged:
					pl.add(rt.NoAction, o.Source, WhyImportDone)
				}
			}
		}
	}

	// Retirement of Gentle-AI: its own mechanism, after verify.
	if opts.RemoveGentleAI && g.Found {
		args, manual := GentleUninstallArgs(g, pl.RuntimeOrder)
		switch {
		case manual:
			pl.note(NoteGentleManual, "")
		case args == nil:
			pl.note(NoteGentleUnmatched, "")
		default:
			pl.GentleArgs = args
			pl.add(rt.RemoveAfterVerify, "gentle-ai "+strings.Join(args, " "), WhyUninstallAll)
		}
	}

	// Always preserved.
	if inv.Memory.Present() {
		db := filepath.Join(inv.Memory.DataDir, "engram.db")
		if !inv.Memory.HasDB {
			db = inv.Memory.DataDir
		}
		pl.add(rt.Preserve, db, WhyEngramAdopted)
		if inv.Memory.ProjectDir {
			pl.add(rt.Preserve, filepath.Join(env.ProjectRoot, ".engram"), WhyEngramProject)
		}
	}
	if g.HasBackups {
		pl.add(rt.Preserve, filepath.Join(GentleHome(env), "backups"), WhyGentleBackups)
	}
	pl.add(rt.NoAction, "user skills and configuration", WhyUserAssets)
	return pl, nil
}

// GentleUninstallArgs returns Gentle-AI's own uninstall invocation. Only
// `uninstall --all` is documented, so it is the only form ever used, and only
// when every agent Gentle-AI manages is one GabitoTL replaces (or Gentle-AI
// records no per-agent state). When `--all` would also remove an agent GabitoTL
// does not replace, args is nil and manual reports whether at least one selected
// runtime matches, so the user is told to run Gentle-AI's own uninstall by hand.
// A nil args with manual false means no selected runtime matches any agent.
func GentleUninstallArgs(g GentleAI, selected []string) (args []string, manual bool) {
	matched := 0
	for _, a := range g.Agents {
		if a.Runtime != "" && slices.Contains(selected, a.Runtime) {
			matched++
		}
	}
	switch {
	case len(g.Agents) == 0, matched == len(g.Agents):
		return []string{"uninstall", "--all"}, false
	case matched == 0:
		return nil, false
	}
	return nil, true
}

// WhyCodes, NoteCodes and WarningCodes list every code presenters must have
// wording for; a test in package ui keeps the messages file complete.
var (
	WhyCodes     = []string{WhyExists, WhySnapshot, WhyEngramAdopted, WhyEngramProject, WhyGentleBackups, WhyUserAssets, WhyUninstallAll, WhyImportDone}
	NoteCodes    = []string{NoteNoRuntimes, NoteRuntimeMissing, NoteEngramMissing, NoteNoMigrator, NoteGentleNotFound, NotePersonaConflict, NoteGentleUnmatched, NoteGentleManual, NoteMemoryUnhealthy, NoteEngramNoBackup}
	WarningCodes = []string{WarnDataDirOverride, WarnCloudSync, WarnProjectConfig, WarnProjectIdentity}
)

// Mutates reports whether running the plan would change anything beyond the
// snapshot itself. A plan that does not mutate is an idempotent re-run: nothing
// to apply, no new snapshot.
func (p Plan) Mutates() bool {
	if len(p.ProjectFiles) > 0 || len(p.Retire) > 0 || p.GentleArgs != nil || p.Options.RemoveGentleAI {
		return true
	}
	for _, it := range p.Items {
		if it.Kind == KindImport {
			return true
		}
	}
	for _, acts := range p.Install {
		for _, a := range acts {
			if a.Kind == rt.Create || a.Kind == rt.Modify {
				return true
			}
		}
	}
	return false
}

// RetireActions flattens the retire-time actions in runtime order.
func (p Plan) RetireActions() []rt.Action {
	var out []rt.Action
	for _, name := range p.RuntimeOrder {
		out = append(out, p.Retire[name]...)
	}
	return out
}
