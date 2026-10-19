package installer

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/identity"
	"github.com/winnstorm/gabitotl/internal/persona"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// Line statuses of a doctor report (state symbols: ✓ ! → ×).
const (
	LineOK    = "ok"
	LineWarn  = "warn"
	LineTodo  = "todo"
	LineError = "error"
)

// Doctor line codes; presenters own the wording.
const (
	DocCoreVersion        = "doctor.core.version"
	DocCorePlatform       = "doctor.core.platform"
	DocCoreGit            = "doctor.core.git"
	DocCoreGitMissing     = "doctor.core.git-missing"
	DocProjectNone        = "doctor.project.none"
	DocProjectMissing     = "doctor.project.missing"
	DocProjectOK          = "doctor.project.ok"
	DocProjectInvalid     = "doctor.project.invalid"
	DocMemEngram          = "doctor.memory.engram"
	DocMemEngramMissing   = "doctor.memory.engram-missing"
	DocMemEngramNoBin     = "doctor.memory.engram-binary-missing"
	DocMemEngramOptional  = "doctor.memory.engram-optional"
	DocMemDoctorOK        = "doctor.memory.doctor-ok"
	DocMemDoctorBad       = "doctor.memory.doctor-bad"
	DocMemProject         = "doctor.memory.project"
	DocMemProjectUnknown  = "doctor.memory.project-unknown"
	DocMemMCP             = "doctor.memory.mcp"
	DocMemMCPNone         = "doctor.memory.mcp-none"
	DocMemMCPPending      = "doctor.memory.mcp-server-pending"
	DocMemTeamOK          = "doctor.memory.team-ok"
	DocMemTeamWarn        = "doctor.memory.team-warn"
	DocMemTeamBad         = "doctor.memory.team-bad"
	DocMemTeamContainer   = "doctor.memory.team-container"
	DocMemTeamNone        = "doctor.memory.team-none"
	DocMemChosen          = "doctor.memory.chosen"
	DocMemMismatch        = "doctor.memory.mismatch"
	DocMemSharedLoopback  = "doctor.memory.shared-loopback"
	DocMemLegacyFields    = "doctor.memory.legacy-fields"
	DocMemTeammate        = "doctor.memory.teammate"
	DocRuntimeOK          = "doctor.runtime.ok"
	DocRuntimeDrift       = "doctor.runtime.drift"
	DocRuntimeTodo        = "doctor.runtime.todo"
	DocRuntimeHistory     = "doctor.runtime.history"
	DocRuntimeConflict    = "doctor.runtime.persona-conflict"
	DocRuntimeGentleKiro  = "doctor.runtime.gentle-kiro"
	DocRuntimePersonaLang = "doctor.runtime.persona-lang"
	DocGitRepo            = "doctor.git.repo"
	DocGitNoRepo          = "doctor.git.no-repo"
	DocGitProvider        = "doctor.git.provider"
	DocGitDelivery        = "doctor.git.delivery"
	DocGitDeliveryTodo    = "doctor.git.delivery-todo"
	DocGitProviderDiffer  = "doctor.git.provider-differs"
	DocGitAuth            = "doctor.git.auth"
	DocGitAuthTodo        = "doctor.git.auth-todo"
	DocGitAuthTool        = "doctor.git.auth-tool"
	DocGitIdentity        = "doctor.git.identity"
	DocGitNoIdentity      = "doctor.git.no-identity"
	DocLegacyGentle       = "doctor.legacy.gentle"
	DocLegacyBackups      = "doctor.legacy.gentle-backups"
	DocLegacySnapshots    = "doctor.legacy.snapshots"
	DocLegacyNone         = "doctor.legacy.none"
)

// DoctorSections are the section names in order (spec 41).
var DoctorSections = []string{"core", "project", "memory", "runtimes", "git", "authorship", "legacy"}

// DoctorCodes lists every line code, for the messages completeness test.
var DoctorCodes = []string{
	DocCoreVersion, DocCorePlatform, DocCoreGit, DocCoreGitMissing, DocProjectNone, DocProjectMissing, DocProjectOK,
	DocProjectInvalid, DocMemEngram, DocMemEngramMissing, DocMemEngramOptional, DocMemEngramNoBin, DocMemDoctorOK, DocMemDoctorBad,
	DocMemProject, DocMemProjectUnknown, DocMemMCP, DocMemMCPNone, DocMemMCPPending, DocMemTeamOK, DocMemTeamWarn, DocMemTeamBad, DocMemTeamContainer, DocMemTeamNone, DocMemChosen, DocMemMismatch, DocMemSharedLoopback, DocMemLegacyFields, DocMemTeammate, DocRuntimeOK, DocRuntimeDrift,
	DocRuntimeTodo, DocRuntimeHistory, DocRuntimeConflict, DocRuntimeGentleKiro, DocRuntimePersonaLang, DocGitRepo, DocGitNoRepo, DocGitProvider, DocGitDelivery, DocGitDeliveryTodo, DocGitProviderDiffer, DocGitAuth, DocGitAuthTodo, DocGitAuthTool, DocGitIdentity,
	DocGitNoIdentity, DocLegacyGentle, DocLegacyBackups, DocLegacySnapshots, DocLegacyNone,
	DocGuardMode, DocGuardHooksOK, DocGuardHooksMissing, DocGuardHooksDeclined, DocGuardRuntimeOK, DocGuardRuntimeTodo,
	DocGuardRuntimeCustom, DocGuardRuntimeNone, DocGuardScanClean, DocGuardScanFound, DocGuardHistory,
}

// Line is one doctor finding.
type Line struct {
	Status string
	Code   string
	Detail string
}

// Section groups lines (Core, Proyecto, Memoria, Runtimes, Git, Legacy).
type Section struct {
	Name  string
	Lines []Line
}

// DoctorReport is the result of Doctor.
type DoctorReport struct{ Sections []Section }

// Errors counts the error lines; the CLI exits 1 when it is not zero.
func (d DoctorReport) Errors() int {
	n := 0
	for _, s := range d.Sections {
		for _, l := range s.Lines {
			if l.Status == LineError {
				n++
			}
		}
	}
	return n
}

// Doctor is strictly read-only: it detects, runs the read-only `engram doctor`
// and `engram projects list`, and re-plans each runtime (Plan is pure). It
// never writes a file.
func Doctor(ctx context.Context, env Env) DoctorReport {
	inv := BuildInventory(ctx, env, nil)
	var d DoctorReport
	add := func(name string, lines ...Line) { d.Sections = append(d.Sections, Section{Name: name, Lines: lines}) }
	L := func(status, code, detail string) Line { return Line{status, code, detail} }

	// Core
	core := []Line{L(LineOK, DocCoreVersion, env.Version), L(LineOK, DocCorePlatform, inv.Platform.OS+"/"+inv.Platform.Arch)}
	if inv.Platform.Git {
		core = append(core, L(LineOK, DocCoreGit, inv.Platform.GitPath))
	} else {
		core = append(core, L(LineError, DocCoreGitMissing, ""))
	}
	add("core", core...)

	// Project
	var proj []Line
	provider := gabitotl.MemoryEngram
	var memCfg gabitotl.MemoryConfig
	var memOrigin gabitotl.MemoryOrigin
	var vcsCfg gabitotl.VCSConfig
	vcsOK := false
	hasProject := false
	switch {
	case env.ProjectRoot == "":
		proj = append(proj, L(LineWarn, DocProjectNone, ""))
	case !inv.Project.Exists:
		proj = append(proj, L(LineWarn, DocProjectMissing, inv.Project.Root))
	default:
		hasProject = true
		l := gabitotl.NewLayout(env.ProjectRoot)
		bad := 0
		check := func(name string, err error) {
			if err != nil {
				bad++
				proj = append(proj, L(LineError, DocProjectInvalid, name+": "+err.Error()))
			}
		}
		_, err := gabitotl.LoadProject(l)
		check("project.md", err)
		m, err := gabitotl.LoadMemory(l)
		check("memory.md", err)
		if err == nil {
			provider, memCfg = m.Provider, m
			memOrigin = gabitotl.InspectMemory(l, identity.OSUserID())
		}
		_, err = gabitotl.LoadRuntime(l)
		check("runtime.md", err)
		vcsCfg, err = gabitotl.LoadVCS(l)
		check("vcs.md", err)
		vcsOK = err == nil
		_, err = gabitotl.LoadWorkflow(l)
		check("workflow.md", err)
		if bad == 0 {
			proj = append([]Line{L(LineOK, DocProjectOK, gabitotl.DirName)}, proj...)
		}
	}
	add("project", proj...)

	// Memory
	var mem []Line
	m := inv.Memory
	// Both providers are reported on their own and a missing one is never fatal on
	// its own: Engram is an error only for a project that uses it and has data it
	// cannot reach; the team server is checked below when the project uses it.
	usesEngram := hasProject && provider == gabitotl.MemoryEngram
	if hasProject {
		mem = append(mem, providerLines(memCfg)...)
		mem = append(mem, originLines(memCfg, memOrigin)...)
	}
	switch {
	case m.HasBinary:
		mem = append(mem, L(LineOK, DocMemEngram, m.Binary))
	case m.Present() && usesEngram:
		mem = append(mem, L(LineError, DocMemEngramNoBin, m.DataDir))
	case m.Present():
		mem = append(mem, L(LineWarn, DocMemEngramNoBin, m.DataDir))
	case usesEngram:
		mem = append(mem, L(LineWarn, DocMemEngramMissing, ""))
	default:
		mem = append(mem, L(LineOK, DocMemEngramOptional, ""))
	}
	if m.DoctorRan {
		if m.DoctorOK {
			mem = append(mem, L(LineOK, DocMemDoctorOK, ""))
		} else {
			var bad []string
			for _, c := range m.DoctorChecks {
				if c.Status == "error" {
					bad = append(bad, c.Name+": "+c.Message)
				}
			}
			st := LineError
			if hasProject && provider != gabitotl.MemoryEngram {
				st = LineWarn // Engram is not what this project uses: reported, not fatal
			}
			mem = append(mem, L(st, DocMemDoctorBad, strings.Join(bad, "; ")))
		}
		if m.ProjectMatch != "" {
			mem = append(mem, L(LineOK, DocMemProject, m.ProjectMatch+" ("+strconv.Itoa(m.Observations)+")"))
		} else if hasProject {
			mem = append(mem, L(LineWarn, DocMemProjectUnknown, inv.Project.Key))
		}
	}
	for _, w := range m.Warnings {
		mem = append(mem, L(LineWarn, "warn."+w.Code, w.Detail))
	}
	if hasProject && provider == gabitotl.MemoryMemoryMCP {
		mem = append(mem, teamDoctor(ctx, env, memCfg)...)
	} else {
		mem = append(mem, L(LineOK, DocMemTeamNone, ""))
	}

	// Runtimes (also tells how many have the memory MCP connected)
	var rts []Line
	renv := env.RT()
	st, _ := renv.Store().Load()
	managed := 0
	plang, _ := gabitotl.LoadPersonaLang(env.Home)
	mcp, extra, retire := rt.MemoryFor(provider, m.HasBinary)
	for _, r := range inv.Runtimes {
		if r.State == rt.StateNone {
			continue
		}
		a, _ := env.Runtimes.Get(r.Name)
		isManaged := len(st.ForRuntime(r.Name)) > 0
		switch {
		case isManaged:
			managed++
			bad := ""
			for _, c := range a.Verify(renv, rt.Options{Scope: rt.ScopeGlobal, PersonaText: persona.TextFor(plang), Skills: SkillAssets(), Memory: mcp, MemoryExtra: extra, RetireMemory: retire, Version: env.Version}) {
				if !c.OK {
					bad = c.Name + ": " + c.Detail
					break
				}
			}
			if bad == "" {
				rts = append(rts, L(LineOK, DocRuntimeOK, r.Label))
			} else {
				rts = append(rts, L(LineError, DocRuntimeDrift, r.Label+" - "+bad))
			}
		case r.Usable():
			rts = append(rts, L(LineTodo, DocRuntimeTodo, r.Label))
		default:
			rts = append(rts, L(LineWarn, DocRuntimeHistory, r.Label))
		}
		if isManaged && gentleKiroCoexists(env, r.Name) {
			rts = append(rts, L(LineWarn, DocRuntimeGentleKiro, r.Name)) // a warning, never a failure: the user may want both
		}
		for _, c := range r.Conflicts {
			rts = append(rts, L(LineWarn, DocRuntimeConflict, r.Label+": "+c.Detail))
		}
	}
	if m.Present() || hasProject {
		if managed > 0 {
			mem = append(mem, L(LineOK, DocMemMCP, strconv.Itoa(managed)))
		} else {
			mem = append(mem, L(LineWarn, DocMemMCPNone, ""))
		}
	}
	add("memory", mem...)
	if managed > 0 {
		rts = append(rts, L(LineOK, DocRuntimePersonaLang, string(plang)))
	}
	add("runtimes", rts...)

	// Git
	var git []Line
	if inv.Project.GitRepo {
		git = append(git, L(LineOK, DocGitRepo, ""))
		if inv.Project.Provider != "" {
			git = append(git, L(LineOK, DocGitProvider, inv.Project.Provider))
		}
	} else if env.ProjectRoot != "" {
		git = append(git, L(LineWarn, DocGitNoRepo, ""))
	}
	// Delivery settings: never a failure, only a pointer to `gabitotl vcs setup`.
	if hasProject && vcsOK {
		if vcsCfg.IsConfigured() {
			git = append(git, L(LineOK, DocGitDelivery, vcsCfg.AutomationLevel()))
		} else {
			git = append(git, L(LineWarn, DocGitDeliveryTodo, ""))
		}
		if inf := inv.Project.Provider; inf != "" && inf != vcsCfg.Provider {
			git = append(git, L(LineWarn, DocGitProviderDiffer, "vcs.md: "+vcsCfg.Provider+", origin: "+inf))
		}
		git = append(git, gitAuthLines(ctx, env, vcsCfg, inv.Project.Provider)...)
	}
	if env.ProjectRoot != "" {
		if u, err := identity.Active(env.ProjectRoot); err == nil {
			git = append(git, L(LineOK, DocGitIdentity, u.Name))
		} else {
			git = append(git, L(LineWarn, DocGitNoIdentity, ""))
		}
	}
	add("git", git...)

	// Authorship and branding (read-only)
	add("authorship", guardDoctor(ctx, env, hasProject, inv)...)

	// Legacy
	var leg []Line
	g := inv.Other.GentleAI
	if g.Found {
		leg = append(leg, L(LineWarn, DocLegacyGentle, g.Version))
	}
	if g.HasBackups {
		leg = append(leg, L(LineWarn, DocLegacyBackups, ""))
	}
	if snaps, _ := ListSnapshots(env); len(snaps) > 0 {
		leg = append(leg, L(LineWarn, DocLegacySnapshots, strconv.Itoa(len(snaps))))
	}
	if len(leg) == 0 {
		leg = append(leg, L(LineOK, DocLegacyNone, ""))
	}
	add("legacy", leg...)
	return d
}

// providerLines reports the memory provider the project chose in memory.md and
// flags a mode that disagrees with the provider (team memory without a server or
// token reference is reported by teamDoctor as a pending setup). Proportional on purpose: it reads the
// file only (teamDoctor does the live checks).
func providerLines(cfg gabitotl.MemoryConfig) []Line {
	out := []Line{{LineOK, DocMemChosen, cfg.Provider + " (" + cfg.Mode + ")"}}
	switch {
	case cfg.Provider == gabitotl.MemoryMemoryMCP && cfg.Mode == gabitotl.ModeSolo:
		out = append(out, Line{LineWarn, DocMemMismatch, "memory.md names memory-mcp but its mode is solo"})
	case cfg.Provider == gabitotl.MemoryEngram && cfg.Mode == gabitotl.ModeTeam:
		out = append(out, Line{LineWarn, DocMemMismatch, "memory.md has mode team but its provider is engram: run `gabitotl memory use memory-mcp`"})
	}
	return out
}

// originLines flags what is wrong with where the team settings are kept: a
// loopback url in the committed file (teammates would inherit an address that only
// works on one machine) and per-machine fields that a legacy file still shares.
func originLines(cfg gabitotl.MemoryConfig, o gabitotl.MemoryOrigin) []Line {
	if cfg.Provider != gabitotl.MemoryMemoryMCP {
		return nil
	}
	var out []Line
	if cfg.Mode == gabitotl.ModeTeam && o.SharedURL != "" && gabitotl.IsLoopbackURL(o.SharedURL) {
		out = append(out, Line{LineWarn, DocMemSharedLoopback, o.SharedURL})
	}
	if len(o.LegacyFields) > 0 {
		out = append(out, Line{LineWarn, DocMemLegacyFields, strings.Join(o.LegacyFields, ", ")})
	}
	return out
}

// teamDoctor is the read-only health check of team memory (provider
// memory-mcp): the server (reachable, token accepted, schema version, project
// key present and allowed) and, on the host, the managed container. It only
// reads: one `memory_doctor` call, one tiny search and `docker inspect`-style
// status queries.
func teamDoctor(ctx context.Context, env Env, cfg gabitotl.MemoryConfig) []Line {
	var out []Line
	if cfg.AwaitsClientSetup() { // a teammate after `git pull`: the server is known, the token is not
		return append(out, Line{LineWarn, DocMemTeammate, cfg.URL})
	}
	if cfg.URL == "" || cfg.Token == "" {
		return append(out, Line{LineWarn, DocMemMCPPending, ""})
	}
	if env.OpenMemory == nil {
		return append(out, Line{LineWarn, DocMemTeamWarn, "no memory client in this build"})
	}
	p, err := env.OpenMemory(cfg)
	if err != nil {
		return append(out, Line{LineError, DocMemTeamBad, err.Error()})
	}
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rep := p.Doctor(dctx)
	for _, c := range rep.Checks {
		st, code := LineOK, DocMemTeamOK
		switch c.Status {
		case "warning":
			st, code = LineWarn, DocMemTeamWarn
		case "error":
			st, code = LineError, DocMemTeamBad
		}
		out = append(out, Line{st, code, strings.TrimSpace(c.Name + ": " + c.Message)})
	}
	if cfg.Role == gabitotl.RoleHost && env.Team.Manager != nil {
		if m, err := env.Team.Manager(cfg.Instance, "auto", 0); err == nil { // the instance recorded in memory.md, if any
			st, err := m.Status(dctx)
			switch {
			case err != nil:
				out = append(out, Line{LineWarn, DocMemTeamContainer, err.Error()})
			default:
				yes := func(b bool) string {
					if b {
						return "yes"
					}
					return "no"
				}
				detail := "installed " + yes(st.Installed) + ", running " + yes(st.Running) + ", ready " + yes(st.Ready) +
					", data volume " + yes(st.VolumeOK) + ", credentials " + yes(st.HasSecrets)
				code := LineOK
				if !st.Installed || !st.Ready {
					code = LineWarn
				}
				out = append(out, Line{code, DocMemTeamContainer, detail})
				for _, c := range st.Conflicts {
					out = append(out, Line{LineError, DocMemTeamContainer, "CONFLICT: " + c})
				}
			}
		}
	}
	return out
}
