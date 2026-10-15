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
	// RetireGentleKiro moves Gentle AI's files in Kiro (steering, skills, agents)
	// into the snapshot and removes them after verify. Gentle AI itself is untouched.
	RetireGentleKiro bool
	PreserveEngram   bool // informational: Engram is always adopted, never copied
	NoGuardHooks     bool // do not install the authorship guard Git hooks (remembered in workflow.md)
	DryRun           bool

	// Team memory (see team.go). Only meaningful with Memory memory-mcp.
	MemoryRole     string   // host | client
	MemoryURL      string   // client: the server endpoint; host: the endpoint teammates will use
	MemoryTokenEnv string   // NAME of the variable that holds the token (default GABITOTL_MEMORY_TOKEN)
	StoreToken     bool     // client: copy the token from the environment variable into the 0600 secrets file
	TokenSecret    string   `json:"-"` // client: a token typed in the TUI, stored in the 0600 file; never printed or persisted elsewhere
	Invites        []string // host: teammates to create a token for (shown once)
	ServerEngine   string   // host: docker | podman | auto
	ServerPort     int      // host: published loopback port (0: automatic)
	ServerInstance string   // host: named instance of the managed server
	ImportEngram   bool     // host: import the Engram memory into the team memory (needs the safety export)
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
	// Gentle AI's files in Kiro: present and kept, or asked for but not found.
	NoteGentleKiroPresent = "gentle-kiro-present"
	NoteGentleKiroNone    = "gentle-kiro-none"
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
	// Gentle AI's files in Kiro, retired into the snapshot after verify.
	WhyGentleKiroSteering = "gentle-kiro-steering"
	WhyGentleKiroSkills   = "gentle-kiro-skills"
	WhyGentleKiroAgents   = "gentle-kiro-agents"
	WhyGentleKiroShared   = "gentle-kiro-shared"
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
	ProjectFiles []string       // .gabitotl files Init would create (absolute paths)
	Memory       rt.MemoryMCP   // the bridge: it routes by the provider of each project
	MemoryExtra  []rt.MemoryMCP // Engram's own entry, when its binary is present
	RetireMemory []string       // memory MCP entries that are obsolete (Engram's entry once its binary is gone)
	Team         *TeamPlan
	MemoryChoice string
	GentleArgs   []string // gentle-ai uninstall arguments, nil when not retiring
	// GentleKiro is Gentle AI's footprint in the Kiro trees of the selected
	// runtimes; nil when there is none. Presenters ask about it and doctor reports it.
	GentleKiro *GentleKiroInventory
	Notes      []Note
}

// Sentinel errors of plan building.
var ErrUnknownRuntime = errors.New("unknown runtime")

// ErrNoReverseMigration refuses moving a project from shared team memory back to
// local Engram: no migrator exists in that direction, so the switch would strand
// the team's memory. memory.md stays as it is.
var ErrNoReverseMigration = errors.New("moving from team memory (memory-mcp) back to Engram is not supported: there is no migrator in that direction, so memory.md stays on memory-mcp")

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
	if pl.MemoryChoice == gabitotl.MemoryEngram && inv.Project.Exists {
		if cur, err := gabitotl.LoadMemory(l); err == nil && cur.Provider == gabitotl.MemoryMemoryMCP {
			return pl, ErrNoReverseMigration
		}
	}
	if err := planTeam(ctx, env, inv, opts, &pl); err != nil {
		return pl, err
	}
	// Both providers are available on every machine; the choice only decides what
	// this project's memory.md says. A missing Engram is a hint, never a failure.
	pl.Memory, pl.MemoryExtra, pl.RetireMemory = rt.MemoryFor(pl.MemoryChoice, inv.Memory.HasBinary)
	if !inv.Memory.HasBinary {
		pl.note(NoteEngramMissing, "")
	}
	if pl.MemoryChoice == gabitotl.MemoryMemoryMCP && inv.Memory.Present() && env.Migrators != nil {
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
				if f == l.Memory() && pl.Team != nil { // the team settings are written into the existing memory.md
					pl.Items = append(pl.Items, PlanItem{Kind: rt.Modify, Target: f, Detail: "team memory settings (references only)"})
					continue
				}
				pl.add(rt.Preserve, f, WhyExists)
				continue
			}
			pl.ProjectFiles = append(pl.ProjectFiles, f)
			pl.add(rt.Create, f, "")
		}
	}

	// Runtime actions, split into install-time and retire-time (invariant b).
	renv := env.RT()
	ropts := rt.Options{Scope: rt.ScopeGlobal, PersonaText: persona.DefaultText(), Memory: pl.Memory, MemoryExtra: pl.MemoryExtra, RetireMemory: pl.RetireMemory, MigrateGentleman: opts.MigratePersona, Version: env.Version}
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
		deferred := err == nil && pendingImport(outs) && teamImportDeferred(ctx, env, l)
		if deferred {
			pl.note(NoteTeamNotReady, "")
		}
		if err == nil {
			for _, o := range outs {
				switch o.Status {
				case importer.StatusWouldApply:
					if deferred { // it cannot be saved now, so it changes nothing: a re-run stays idempotent
						continue
					}
					pl.Items = append(pl.Items, PlanItem{Kind: KindImport, Target: o.Source, Count: len(o.Items)})
				case importer.StatusUnchanged:
					pl.add(rt.NoAction, o.Source, WhyImportDone)
				}
			}
		}
	}

	planGentleKiro(env, opts, &pl)

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
	WhyCodes     = []string{WhyExists, WhySnapshot, WhyEngramAdopted, WhyEngramProject, WhyGentleBackups, WhyUserAssets, WhyUninstallAll, WhyImportDone, WhyGentleKiroSteering, WhyGentleKiroSkills, WhyGentleKiroAgents, WhyGentleKiroShared}
	NoteCodes    = []string{NoteNoContainerEngine, NoteServerConflict, NoteTeamUnconfigured, NoteImportUnavailable, NoteImportNeedsHost, NoteTeamNotReady, NoteNoRuntimes, NoteRuntimeMissing, NoteEngramMissing, NoteNoMigrator, NoteGentleNotFound, NotePersonaConflict, NoteGentleUnmatched, NoteGentleManual, NoteMemoryUnhealthy, NoteEngramNoBackup, NoteGentleKiroPresent, NoteGentleKiroNone, NoteTeamURLMissing, NoteTeamTokenMissing, NoticeImportSkippedHost, NoticeImportSkippedClient, NoticeImportSkippedNoToken}
	WarningCodes = []string{WarnDataDirOverride, WarnCloudSync, WarnProjectConfig, WarnProjectIdentity}
)

// Mutates reports whether running the plan would change anything beyond the
// snapshot itself. A plan that does not mutate is an idempotent re-run: nothing
// to apply, no new snapshot.
func (p Plan) Mutates() bool {
	if len(p.ProjectFiles) > 0 || len(p.Retire) > 0 || p.GentleArgs != nil || p.Options.RemoveGentleAI || p.retireGentleKiro() || (p.Team != nil && !p.Team.Unchanged) {
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

func pendingImport(outs []importer.Outcome) bool {
	return slices.ContainsFunc(outs, func(o importer.Outcome) bool { return o.Status == importer.StatusWouldApply })
}

// teamImportDeferred reports whether a knowledge import would go to a team
// memory that cannot take writes right now (server down, token rejected). The
// probe is one read-only doctor call against the memory.md that already exists.
func teamImportDeferred(ctx context.Context, env Env, l gabitotl.Layout) bool {
	cfg, err := gabitotl.LoadMemory(l)
	if err != nil || cfg.Provider != gabitotl.MemoryMemoryMCP || cfg.URL == "" || env.OpenMemory == nil {
		return false
	}
	p, err := env.OpenMemory(cfg)
	return err == nil && teamNotReady(ctx, p) != ""
}
