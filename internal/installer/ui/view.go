package ui

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/winnstorm/gabitotl/internal/installer"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

var osNames = map[string]string{"linux": "Linux", "darwin": "macOS", "windows": "Windows"}

func line(w io.Writer, sym, text string) { fmt.Fprintf(w, "  %s %s\n", sym, text) }

func box(w io.Writer, title, subtitle string) {
	const width = 40
	pad := func(s string) string {
		n := utf8.RuneCountInString(s)
		l := (width - n) / 2
		if l < 0 {
			l = 0
		}
		r := width - n - l
		if r < 0 {
			r = 0
		}
		return strings.Repeat(" ", l) + s + strings.Repeat(" ", r)
	}
	fmt.Fprintf(w, "╭%s╮\n│%s│\n│%s│\n╰%s╯\n", strings.Repeat("─", width), pad(title), pad(subtitle), strings.Repeat("─", width))
}

// Line is one symbol-prefixed row of a screen.
type Line struct{ Sym, Text string }

// Section is a titled group of lines (system, agents, memory, other harness).
type Section struct {
	Title string
	Lines []Line
}

// InventorySections is the detection summary (spec 2) as data, so the plain
// renderer and the TUI show exactly the same rows.
func InventorySections(m Messages, inv installer.Inventory) []Section {
	sys := Section{Title: m.Format("inv.section.system")}
	os := osNames[inv.Platform.OS]
	if os == "" {
		os = inv.Platform.OS
	}
	sys.Lines = append(sys.Lines, Line{SymOK, m.Format("inv.os", "name", os)})
	if inv.Platform.Git {
		sys.Lines = append(sys.Lines, Line{SymOK, m.Format("inv.git")})
	} else {
		sys.Lines = append(sys.Lines, Line{SymError, m.Format("inv.git.missing")})
	}
	if inv.Project.Root != "" {
		sys.Lines = append(sys.Lines, Line{SymOK, m.Format("inv.project", "name", inv.Project.Name)})
	} else {
		sys.Lines = append(sys.Lines, Line{SymWarn, m.Format("inv.project.none")})
	}

	agents := Section{Title: m.Format("inv.section.agents")}
	for _, r := range inv.Runtimes {
		switch {
		case r.Usable():
			agents.Lines = append(agents.Lines, Line{SymOK, m.Format("inv.runtime.ready", "name", r.Label)})
		case r.State == rt.StateDetectedHistory:
			agents.Lines = append(agents.Lines, Line{SymWarn, m.Format("inv.runtime.history", "name", r.Label)})
		}
	}

	memS := Section{Title: m.Format("inv.section.memory")}
	mem := inv.Memory
	switch {
	case mem.Present():
		memS.Lines = append(memS.Lines, Line{SymOK, m.Format("inv.engram.found")})
		if mem.Observations > 0 {
			memS.Lines = append(memS.Lines, Line{SymOK, m.Format("inv.engram.obs", "count", strconv.Itoa(mem.Observations))})
		}
		if mem.DoctorRan && mem.DoctorOK {
			memS.Lines = append(memS.Lines, Line{SymOK, m.Format("inv.engram.healthy")})
		} else if mem.DoctorRan {
			memS.Lines = append(memS.Lines, Line{SymError, m.Format("inv.engram.unhealthy")})
		}
	default:
		memS.Lines = append(memS.Lines, Line{SymWarn, m.Format("inv.engram.none")})
	}

	other := Section{Title: m.Format("inv.section.other")}
	if g := inv.Other.GentleAI; g.Found {
		other.Lines = append(other.Lines, Line{SymWarn, strings.TrimSpace(m.Format("inv.gentle.found", "detail", g.Version))})
		if len(g.Agents) > 0 {
			var ids []string
			for _, a := range g.Agents {
				ids = append(ids, a.ID)
			}
			other.Lines = append(other.Lines, Line{" ", m.Format("inv.gentle.agents", "detail", strings.Join(ids, ", "))})
		}
	} else {
		other.Lines = append(other.Lines, Line{SymOK, m.Format("inv.other.none")})
	}
	return []Section{sys, agents, memS, other}
}

// RenderInventory prints the detection summary (spec 2): what was found, not
// what will be asked.
func RenderInventory(w io.Writer, m Messages, inv installer.Inventory) {
	box(w, m.Format("inv.title"), m.Format("inv.subtitle"))
	for _, s := range InventorySections(m, inv) {
		fmt.Fprintf(w, "\n%s\n", s.Title)
		for _, l := range s.Lines {
			line(w, l.Sym, l.Text)
		}
	}
	fmt.Fprintln(w)
}

// RenderOjoMigration prints the Gabito "Ojo" before a Gentle-AI migration.
func RenderOjoMigration(w io.Writer, m Messages, inv installer.Inventory) {
	key, pairs := "ojo.migration.nomem", []string(nil)
	if inv.Memory.Observations > 0 {
		key, pairs = "ojo.migration", []string{"count", strconv.Itoa(inv.Memory.Observations)}
	}
	fmt.Fprintf(w, "%s:\n%s\n\n", m.Format("label.ojo"), m.Format(key, pairs...))
}

var lineSymbols = map[string]string{
	installer.LineOK: SymOK, installer.LineWarn: SymWarn, installer.LineTodo: SymTodo, installer.LineError: SymError,
}

// RenderDoctor prints the doctor report grouped by section (spec 41).
func RenderDoctor(w io.Writer, m Messages, d installer.DoctorReport) {
	fmt.Fprintf(w, "%s\n", m.Format("doctor.title"))
	for _, s := range d.Sections {
		if len(s.Lines) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s\n", m.Format("doctor.section."+s.Name))
		for _, l := range s.Lines {
			fmt.Fprintf(w, "%s %s\n", lineSymbols[l.Status], m.Format(l.Code, "detail", l.Detail))
		}
	}
	if n := d.Errors(); n > 0 {
		fmt.Fprintf(w, "\n%s %s\n", SymError, m.Format("doctor.summary.errors", "count", strconv.Itoa(n)))
	} else {
		fmt.Fprintf(w, "\n%s %s\n", SymOK, m.Format("doctor.summary.ok"))
	}
}

// RenderEvent prints one progress event: phase starts as "→ ...", warnings as "! ...".
func RenderEvent(w io.Writer, m Messages, e installer.Event) {
	switch {
	case e.Status == "start":
		if msg, ok := m["phase."+string(e.Phase)]; ok {
			fmt.Fprintf(w, "%s %s…\n", SymTodo, msg)
		}
	case e.Status == "warn":
		fmt.Fprintf(w, "%s %s %s\n", SymWarn, e.Step, e.Detail)
	}
}

// RenderResult prints the closing summary of a run (spec 28, final screen).
func RenderResult(w io.Writer, m Messages, res installer.Result, inv installer.Inventory, paths Paths) {
	switch {
	case res.RolledBack:
		fmt.Fprintf(w, "%s %s\n", SymError, m.Format("result.rolled_back", "count", strconv.Itoa(len(res.RolledBackWith))))
	case res.Skipped:
		fmt.Fprintf(w, "%s %s\n", SymOK, m.Format("result.nothing"))
	case res.DryRun:
		fmt.Fprintln(w, m.Format(KeyPlanDryDone))
	default:
		fmt.Fprintf(w, "\n%s\n\n%s\n%s %s\n", m.Format("result.done"), m.Format("result.gabitotl"), SymOK, m.Format("result.configured"))
		if inv.Memory.Present() && res.Team == nil {
			fmt.Fprintf(w, "\n%s\n%s %s\n", m.Format("result.memory"), SymOK, m.Format("result.memory.reused"))
			if inv.Memory.Observations > 0 {
				fmt.Fprintf(w, "%s %s\n", SymOK, m.Format("result.memory.preserved", "count", strconv.Itoa(inv.Memory.Observations)))
			}
		}
		renderTeam(w, m, res, paths)
		if len(res.Applied) > 0 {
			fmt.Fprintf(w, "\n%s\n", m.Format("result.runtimes"))
			for _, n := range res.Applied {
				label := n
				if r, ok := inv.Runtime(n); ok {
					label = r.Label
				}
				fmt.Fprintf(w, "%s %s\n", SymOK, label)
			}
		}
		if inv.Other.GentleAI.Found {
			fmt.Fprintf(w, "\n%s\n", m.Format("result.gentle"))
			if len(res.Retired.GentleArgs) > 0 || len(res.Retired.Personas) > 0 {
				fmt.Fprintf(w, "%s %s\n", SymOK, m.Format("result.gentle.retired"))
			} else {
				fmt.Fprintf(w, "%s %s\n", SymOK, m.Format("result.gentle.kept"))
			}
		}
		if res.Snapshot != nil {
			fmt.Fprintf(w, "\n%s\n", m.Format("result.snapshot", "path", paths.Short(res.Snapshot.Dir)))
		}
		if inv.Other.GentleAI.Found {
			fmt.Fprintf(w, "\n%s:\n%s\n", m.Format("label.tip"), m.Format("tip.backup"))
		}
	}
	for _, wn := range res.Warnings {
		fmt.Fprintf(w, "%s %s\n", SymWarn, m.Format("result.warning", "detail", wn))
	}
}

// RenderOutcome prints the closing report of a run and, when it failed, the
// failure line on errw. A run that failed before anything was configured and
// was not rolled back (a failed snapshot, for example) prints only the failure:
// there is no "Listo" to show.
func RenderOutcome(w, errw io.Writer, m Messages, res installer.Result, inv installer.Inventory, paths Paths, err error) {
	var partial *installer.PartialError
	isPartial := errors.As(err, &partial)
	if err == nil || res.RolledBack || isPartial {
		RenderResult(w, m, res, inv, paths)
	}
	if err == nil {
		return
	}
	key := "result.failed"
	if isPartial {
		key = "result.partial"
	}
	fmt.Fprintf(errw, "%s %s\n", SymError, m.Format(key, "detail", err.Error()))
}

// RenderSnapshots lists migration snapshots for `gabitotl restore`.
func RenderSnapshots(w io.Writer, m Messages, snaps []*installer.Snapshot) {
	if len(snaps) == 0 {
		fmt.Fprintln(w, m.Format("restore.none"))
		return
	}
	fmt.Fprintln(w, m.Format("restore.title"))
	for _, s := range snaps {
		detail := fmt.Sprintf("%s, %d archivo(s)", s.Manifest.Source, len(s.Manifest.Files))
		fmt.Fprintf(w, "  %s\n", m.Format("restore.line", "id", s.ID, "status", s.Manifest.Status, "detail", detail))
	}
	fmt.Fprintf(w, "\n%s\n", m.Format("restore.hint"))
}

// RenderRestorePlan lists what a restore would change.
func RenderRestorePlan(w io.Writer, m Messages, changes []installer.RestoreChange, paths Paths) {
	if len(changes) == 0 {
		fmt.Fprintf(w, "%s %s\n", SymOK, m.Format("restore.nothing"))
		return
	}
	fmt.Fprintln(w, m.Format("restore.plan"))
	for _, c := range changes {
		fmt.Fprintf(w, "  %s %s %s\n", SymTodo, m.Format("restore.change."+c.Action), paths.Short(c.Path))
	}
	fmt.Fprintf(w, "\n%s\n", m.Format("restore.note"))
}

// RenderUninstallPlan shows what uninstall removes and what it keeps (spec 46).
func RenderUninstallPlan(w io.Writer, m Messages, pl installer.UninstallPlan, paths Paths) {
	if len(pl.Runtimes) == 0 {
		fmt.Fprintf(w, "%s %s\n", SymOK, m.Format("uninstall.none"))
		return
	}
	fmt.Fprintf(w, "%s\n\n", m.Format("uninstall.remove"))
	for _, n := range pl.Runtimes {
		files := append([]string(nil), pl.Files[n]...)
		sort.Strings(files)
		for _, f := range files {
			fmt.Fprintf(w, "%s %s\n", SymTodo, m.Format("uninstall.remove.item", "name", n, "detail", paths.Short(f)))
		}
	}
	fmt.Fprintf(w, "\n%s\n\n", m.Format("uninstall.keep"))
	for _, k := range pl.Preserved {
		fmt.Fprintf(w, "%s %s\n", SymOK, m.Format("uninstall.keep."+k))
	}
}

// RenderCleanup lists expired legacy material.
func RenderCleanup(w io.Writer, m Messages, items []installer.Expired, days int, paths Paths) {
	if len(items) == 0 {
		fmt.Fprintf(w, "%s %s\n", SymOK, m.Format("cleanup.none"))
		return
	}
	fmt.Fprintf(w, "%s\n", m.Format("cleanup.title", "days", strconv.Itoa(days)))
	for _, it := range items {
		fmt.Fprintf(w, "  %s %s\n", SymTodo, m.Format("cleanup.item", "kind", it.Kind, "path", paths.Short(it.Path), "age", strconv.Itoa(it.AgeDays)))
	}
	fmt.Fprintf(w, "\n%s\n", m.Format("cleanup.note"))
}

// ReportRenderer returns the Spanish renderer of the migration report written
// to .gabitotl/imports.md (spec 22). Inject it as installer.Env.RenderReport.
func ReportRenderer(m Messages) func(installer.Report) string {
	return func(r installer.Report) string {
		var b strings.Builder
		fmt.Fprintf(&b, "- source: %s\n- imported_at: %s\n- status: %s\n\n# %s\n\n## %s\n\n", r.Source, r.ImportedAt, r.Status, titleOf(r.Source), m.Format("report.memory"))
		if r.Engram {
			fmt.Fprintf(&b, "%s\n\n", m.Format("report.engram.reused"))
			if r.Project != "" {
				fmt.Fprintf(&b, "%s\n%s\n\n", m.Format("report.engram.project"), r.Project)
			}
			if r.Observation > 0 {
				fmt.Fprintf(&b, "%s\n%d\n\n", m.Format("report.engram.obs"), r.Observation)
			}
			if r.Backup != "" {
				fmt.Fprintf(&b, "%s\n%s\n\n", m.Format("report.engram.backup"), r.Backup)
			}
		} else {
			fmt.Fprintf(&b, "%s\n\n", m.Format("report.engram.none"))
		}
		if r.MemoryNote != "" {
			fmt.Fprintf(&b, "%s\n\n", m.Format("report.memory.note."+r.MemoryNote))
		}
		list := func(key string, items []string) {
			fmt.Fprintf(&b, "## %s\n\n", m.Format(key))
			for _, i := range items {
				fmt.Fprintf(&b, "- %s\n", i)
			}
			if len(items) == 0 {
				fmt.Fprintf(&b, "- %s\n", m.Format("report.none"))
			}
			b.WriteString("\n")
		}
		list("report.imported", r.Imported)
		var not, ret, kept []string
		for _, n := range r.NotImported {
			if rest, ok := strings.CutPrefix(n, "components: "); ok {
				not = append(not, m.Format("report.not.components", "detail", rest))
			} else {
				not = append(not, m.Format("report.not."+n))
			}
		}
		for _, x := range r.Retired {
			switch {
			case strings.HasPrefix(x, "persona: "):
				ret = append(ret, m.Format("report.retired.persona", "detail", strings.TrimPrefix(x, "persona: ")))
			case strings.HasPrefix(x, "gentle-ai: "):
				ret = append(ret, m.Format("report.retired.gentle", "detail", strings.TrimPrefix(x, "gentle-ai: ")))
			}
		}
		for _, k := range r.Preserved {
			kept = append(kept, m.Format("report.kept."+k))
		}
		list("report.not_imported", not)
		list("report.retired", ret)
		list("report.preserved", kept)
		return strings.TrimRight(b.String(), "\n") + "\n"
	}
}

func titleOf(source string) string {
	if source == installer.SourceGentleAI {
		return "Gentle-AI"
	}
	return strings.ToUpper(source[:1]) + source[1:]
}

// renderTeam prints the team-memory part of the result: what the host set up,
// how teammates connect, the one-time tokens and the import outcome. Tokens
// appear here once and nowhere else.
func renderTeam(w io.Writer, m Messages, res installer.Result, paths Paths) {
	t := res.Team
	if t == nil {
		return
	}
	fmt.Fprintf(w, "\n%s\n", m.Format("result.team"))
	switch t.Role {
	case "host":
		if sv := res.Server; sv != nil {
			fmt.Fprintf(w, "%s %s\n", SymOK, m.Format("result.team.host", "port", strconv.Itoa(sv.Port)))
			if sv.Created {
				fmt.Fprintf(w, "%s %s\n", SymOK, m.Format("result.team.host_created"))
			} else {
				fmt.Fprintf(w, "%s %s\n", SymOK, m.Format("result.team.host_reused"))
			}
		}
		fmt.Fprintf(w, "%s %s\n", SymOK, m.Format("result.team.token_stored"))
		fmt.Fprintf(w, "%s %s\n", SymTodo, m.Format("result.team.serve"))
		fmt.Fprintf(w, "%s %s\n", SymTodo, m.Format("result.team.connect", "url", t.URL, "env", t.TokenEnv, "project", t.Project))
		if len(res.Tokens) > 0 {
			fmt.Fprintf(w, "\n%s %s\n", SymWarn, m.Format("result.team.invites"))
			for _, tk := range res.Tokens {
				fmt.Fprintf(w, "  %s  %s\n", tk.User, tk.Token)
			}
		}
	case "client":
		fmt.Fprintf(w, "%s %s\n", SymOK, m.Format("result.team.client", "url", t.URL, "env", t.TokenEnv))
	}
	if im := res.Import; im != nil {
		sample := fmt.Sprintf("%d/%d", im.SampleOK, im.SampleChecked)
		fmt.Fprintf(w, "%s %s\n", SymOK, m.Format("result.team.import", "imported", strconv.Itoa(im.Imported), "already", strconv.Itoa(im.AlreadyPresent), "sample", sample))
		if res.Snapshot != nil {
			fmt.Fprintf(w, "%s %s\n", SymOK, m.Format("result.team.import_report", "path", paths.Short(filepath.Join(res.Snapshot.Dir, "memory", "import-report.json"))))
		}
		fmt.Fprintf(w, "%s %s\n", SymOK, m.Format("result.team.import_note"))
	}
}
