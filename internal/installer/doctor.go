package installer

import (
	"context"
	"strconv"
	"strings"

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
	DocCoreVersion       = "doctor.core.version"
	DocCorePlatform      = "doctor.core.platform"
	DocCoreGit           = "doctor.core.git"
	DocCoreGitMissing    = "doctor.core.git-missing"
	DocProjectNone       = "doctor.project.none"
	DocProjectMissing    = "doctor.project.missing"
	DocProjectOK         = "doctor.project.ok"
	DocProjectInvalid    = "doctor.project.invalid"
	DocMemEngram         = "doctor.memory.engram"
	DocMemEngramMissing  = "doctor.memory.engram-missing"
	DocMemEngramNoBin    = "doctor.memory.engram-binary-missing"
	DocMemDoctorOK       = "doctor.memory.doctor-ok"
	DocMemDoctorBad      = "doctor.memory.doctor-bad"
	DocMemProject        = "doctor.memory.project"
	DocMemProjectUnknown = "doctor.memory.project-unknown"
	DocMemMCP            = "doctor.memory.mcp"
	DocMemMCPNone        = "doctor.memory.mcp-none"
	DocMemMCPPending     = "doctor.memory.mcp-server-pending"
	DocRuntimeOK         = "doctor.runtime.ok"
	DocRuntimeDrift      = "doctor.runtime.drift"
	DocRuntimeTodo       = "doctor.runtime.todo"
	DocRuntimeHistory    = "doctor.runtime.history"
	DocRuntimeConflict   = "doctor.runtime.persona-conflict"
	DocGitRepo           = "doctor.git.repo"
	DocGitNoRepo         = "doctor.git.no-repo"
	DocGitProvider       = "doctor.git.provider"
	DocGitIdentity       = "doctor.git.identity"
	DocGitNoIdentity     = "doctor.git.no-identity"
	DocLegacyGentle      = "doctor.legacy.gentle"
	DocLegacyBackups     = "doctor.legacy.gentle-backups"
	DocLegacySnapshots   = "doctor.legacy.snapshots"
	DocLegacyNone        = "doctor.legacy.none"
)

// DoctorSections are the section names in order (spec 41).
var DoctorSections = []string{"core", "project", "memory", "runtimes", "git", "legacy"}

// DoctorCodes lists every line code, for the messages completeness test.
var DoctorCodes = []string{
	DocCoreVersion, DocCorePlatform, DocCoreGit, DocCoreGitMissing, DocProjectNone, DocProjectMissing, DocProjectOK,
	DocProjectInvalid, DocMemEngram, DocMemEngramMissing, DocMemEngramNoBin, DocMemDoctorOK, DocMemDoctorBad,
	DocMemProject, DocMemProjectUnknown, DocMemMCP, DocMemMCPNone, DocMemMCPPending, DocRuntimeOK, DocRuntimeDrift,
	DocRuntimeTodo, DocRuntimeHistory, DocRuntimeConflict, DocGitRepo, DocGitNoRepo, DocGitProvider, DocGitIdentity,
	DocGitNoIdentity, DocLegacyGentle, DocLegacyBackups, DocLegacySnapshots, DocLegacyNone,
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
			provider = m.Provider
		}
		_, err = gabitotl.LoadRuntime(l)
		check("runtime.md", err)
		_, err = gabitotl.LoadVCS(l)
		check("vcs.md", err)
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
	switch {
	case m.HasBinary:
		mem = append(mem, L(LineOK, DocMemEngram, m.Binary))
	case m.Present():
		mem = append(mem, L(LineError, DocMemEngramNoBin, m.DataDir))
	case provider == gabitotl.MemoryEngram:
		mem = append(mem, L(LineWarn, DocMemEngramMissing, ""))
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
			mem = append(mem, L(LineError, DocMemDoctorBad, strings.Join(bad, "; ")))
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
	if provider == gabitotl.MemoryMemoryMCP {
		mem = append(mem, L(LineWarn, DocMemMCPPending, ""))
	}

	// Runtimes (also tells how many have the memory MCP connected)
	var rts []Line
	renv := env.RT()
	st, _ := renv.Store().Load()
	managed := 0
	mcp := rt.MemoryMCP{}
	if provider == gabitotl.MemoryEngram && m.Present() {
		mcp = rt.MemoryMCP{Name: "engram", Command: "engram", Args: []string{"mcp"}}
	}
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
			for _, c := range a.Verify(renv, rt.Options{Scope: rt.ScopeGlobal, PersonaText: persona.DefaultText(), Memory: mcp, Version: env.Version}) {
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
		for _, c := range r.Conflicts {
			rts = append(rts, L(LineWarn, DocRuntimeConflict, r.Label+": "+c.Detail))
		}
	}
	if provider == gabitotl.MemoryEngram && m.Present() {
		if managed > 0 {
			mem = append(mem, L(LineOK, DocMemMCP, strconv.Itoa(managed)))
		} else {
			mem = append(mem, L(LineWarn, DocMemMCPNone, ""))
		}
	}
	add("memory", mem...)
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
	if env.ProjectRoot != "" {
		if u, err := identity.Active(env.ProjectRoot); err == nil {
			git = append(git, L(LineOK, DocGitIdentity, u.Name))
		} else {
			git = append(git, L(LineWarn, DocGitNoIdentity, ""))
		}
	}
	add("git", git...)

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
