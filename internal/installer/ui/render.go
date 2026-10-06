package ui

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/winnstorm/gabitotl/internal/installer"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// Paths shortens absolute paths for display: the home directory becomes ~ and
// paths inside the project become project-relative.
type Paths struct{ Home, Root string }

// Tilde replaces every occurrence of the home directory inside text with ~.
func (s Paths) Tilde(text string) string {
	if s.Home == "" {
		return text
	}
	return strings.ReplaceAll(text, s.Home, "~")
}

// Short returns the display form of p.
func (s Paths) Short(p string) string {
	if s.Root != "" && strings.HasPrefix(p, s.Root+string(filepath.Separator)) {
		return strings.TrimPrefix(p, s.Root+string(filepath.Separator))
	}
	if s.Home != "" && (p == s.Home || strings.HasPrefix(p, s.Home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(p, s.Home)
	}
	return p
}

// planGroups is the order and spelling of the dry-run output (spec 42).
var planGroups = []struct {
	kind  rt.Kind
	label string
}{
	{rt.Create, "CREATE"},
	{rt.Modify, "MODIFY"},
	{rt.Preserve, "PRESERVE"},
	{installer.KindImport, "IMPORT"},
	{rt.RemoveAfterVerify, "REMOVE AFTER VERIFY"},
	{rt.NoAction, "NO ACTION"},
}

// PlanGroup is one verb of the plan with its display lines.
type PlanGroup struct {
	Kind  rt.Kind
	Label string
	Lines []string // "<target>  - <why>", no indentation
}

// PlanGroups returns the non-empty groups of the plan in the spec-42 order. The
// plain renderer and the TUI both read it.
func PlanGroups(m Messages, pl installer.Plan, paths Paths) []PlanGroup {
	var out []PlanGroup
	for _, g := range planGroups {
		var lines []string
		for _, it := range pl.Items {
			if it.Kind != g.kind {
				continue
			}
			target := paths.Short(it.Target)
			if strings.Contains(target, " [") { // "<path> [section x]": shorten the path and any path inside the label
				p, rest, _ := strings.Cut(it.Target, " [")
				target = paths.Short(p) + " [" + paths.Tilde(rest)
			}
			line := target
			switch {
			case it.Why == "" && it.Count > 0:
				line += "  - " + m.Format(KeyPlanImport, "count", strconv.Itoa(it.Count))
			case it.Why != "":
				line += "  - " + m.Format("why."+it.Why)
			case it.Detail != "":
				line += "  - " + it.Detail
			}
			lines = append(lines, line)
		}
		if len(lines) > 0 {
			out = append(out, PlanGroup{Kind: g.kind, Label: g.label, Lines: lines})
		}
	}
	return out
}

// RenderPlan prints the plan grouped as CREATE / MODIFY / PRESERVE / IMPORT /
// REMOVE AFTER VERIFY / NO ACTION. It is pure output: nothing is changed.
func RenderPlan(w io.Writer, m Messages, pl installer.Plan, paths Paths) {
	fmt.Fprintln(w, m.Format(KeyPlanTitle))
	groups := PlanGroups(m, pl, paths)
	for _, g := range groups {
		fmt.Fprintf(w, "\n%s\n", g.Label)
		for _, l := range g.Lines {
			fmt.Fprintf(w, "  %s\n", l)
		}
	}
	if len(groups) == 0 {
		fmt.Fprintln(w, m.Format(KeyPlanEmpty))
	}
}

// RenderNotes prints the plan notes and the Engram topology warnings, each as
// an "Ojo" line (symbol !) in the Gabito voice.
func RenderNotes(w io.Writer, m Messages, notes []installer.Note, warns []installer.Warning) {
	for _, n := range notes {
		fmt.Fprintf(w, "%s %s\n", SymWarn, m.Format("note."+n.Code, "detail", n.Detail))
	}
	for _, x := range warns {
		fmt.Fprintf(w, "%s %s\n", SymWarn, m.Format("warn."+x.Code, "detail", x.Detail))
	}
}
