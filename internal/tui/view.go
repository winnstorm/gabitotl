package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

const (
	minWidth  = 24
	minHeight = 6
)

// View implements tea.Model.
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

// frame is one screen before windowing: logical lines, the line the cursor is
// on (-1 for none) and the footer help.
type frame struct {
	title, subtitle string
	body            []string
	focus           int
	help            string
}

// wrapped wraps the logical lines to the width and returns the physical lines
// plus the index of the first physical line of focus.
func wrapped(lines []string, focus, width int) ([]string, int) {
	var out []string
	at := -1
	for i, l := range lines {
		if i == focus {
			at = len(out)
		}
		hang := hanging(l)
		if hang == 0 || hang >= width/2 {
			out = append(out, strings.Split(lipgloss.Wrap(l, width, ""), "\n")...)
			continue
		}
		// Keep continuation lines aligned under the text, not under the symbol.
		parts := strings.Split(lipgloss.Wrap(l, width, ""), "\n")
		out = append(out, parts[0])
		if len(parts) > 1 {
			rest := strings.TrimSpace(strings.Join(parts[1:], " "))
			for _, w := range strings.Split(lipgloss.Wrap(rest, width-hang, ""), "\n") {
				out = append(out, strings.Repeat(" ", hang)+w)
			}
		}
	}
	return out, at
}

// hanging is how far the text of a "<spaces><symbol> text" line is indented.
func hanging(l string) int {
	plain := ansi.Strip(l)
	trimmed := strings.TrimLeft(plain, " ")
	for _, sym := range []string{ui.SymOK, ui.SymWarn, ui.SymTodo, ui.SymError} {
		if strings.HasPrefix(trimmed, sym+" ") {
			return len(plain) - len(trimmed) + 2
		}
	}
	return 0
}

// head is the wrapped header; title and subtitle only matter for its height.
func (m *Model) head(title, subtitle string) []string {
	var out []string
	for _, l := range m.st.header(title, subtitle, m.w, m.h) {
		out = append(out, strings.Split(lipgloss.Wrap(l, max(10, m.w-1), ""), "\n")...)
	}
	return out
}

func (m *Model) bodyRows() int {
	_, _, fr := m.physical()
	chrome := 3 // scroll hint, blank, footer
	if !m.compact() {
		chrome++ // blank under the header
	}
	foot := len(strings.Split(lipgloss.Wrap(fr.help, max(10, m.w-1), ""), "\n")) - 1
	return max(3, m.h-len(m.head(fr.title, fr.subtitle))-chrome-foot)
}

func (m *Model) physical() ([]string, int, frame) {
	fr := m.frame()
	lines, at := wrapped(fr.body, fr.focus, max(10, m.w-1))
	return lines, at, fr
}

func (m *Model) clampScroll() {
	lines, _, _ := m.physical()
	m.scroll = max(0, min(m.scroll, len(lines)-m.bodyRows()))
}

// ensureFocus scrolls just enough to keep the cursor line visible. It runs
// when the cursor or the size changes, so manual scrolling is never undone.
func (m *Model) ensureFocus() {
	_, at, _ := m.physical()
	rows := m.bodyRows()
	switch {
	case at < 0:
	case at < m.scroll:
		m.scroll = at
	case at >= m.scroll+rows:
		m.scroll = at - rows + 1
	}
	m.clampScroll()
}

func (m *Model) render() string {
	if m.w < minWidth || m.h < minHeight {
		return lipgloss.Wrap(m.msg.Format("tui.too_small"), max(1, m.w), "")
	}
	lines, _, fr := m.physical()
	rows := m.bodyRows()
	start := max(0, min(m.scroll, len(lines)-rows))
	end := min(len(lines), start+rows)
	var b strings.Builder
	for _, l := range m.head(fr.title, fr.subtitle) {
		b.WriteString(l + "\n")
	}
	if !m.compact() {
		b.WriteString("\n")
	}
	b.WriteString(strings.Join(lines[start:end], "\n"))
	if start > 0 || end < len(lines) { // there is more: say so with a symbol, not with color
		b.WriteString("\n" + m.st.dim.Render(m.moreHint(start > 0, end < len(lines))))
	}
	b.WriteString("\n\n" + m.st.dim.Render(lipgloss.Wrap(fr.help, max(10, m.w-1), "")))
	return b.String()
}

func (m *Model) moreHint(up, down bool) string {
	switch {
	case up && down:
		return "↑↓"
	case up:
		return "↑"
	}
	return "↓"
}

func (m *Model) frame() frame {
	f := m.msg.Format
	fr := frame{title: f("inv.title"), subtitle: f("inv.subtitle"), focus: -1, help: f("tui.help.menu")}
	switch m.screen {
	case scrDetect, scrBuilding:
		fr.body = []string{f("tui.detecting")}
		fr.help = f("tui.help.menu")
	case scrHome:
		fr.body, fr.focus = m.homeBody()
	case scrDetails:
		fr.subtitle = f("tui.details.title")
		fr.body, fr.help = m.detailsBody(), f("tui.help.scroll")
	case scrMemory:
		fr.subtitle = f("tui.memory.title")
		fr.body, fr.focus = m.memoryBody()
	case scrTeam:
		fr.subtitle = f("tui.team.title")
		fr.body, fr.focus = m.teamBody()
	case scrEngram:
		fr.subtitle = f("tui.engram.title")
		fr.body, fr.focus = m.engramBody()
	case scrForm:
		fr.body, fr.help = strings.Split(m.form.View(), "\n"), f("tui.help.form")
		if m.formKind == "runtimes" {
			fr.help = f("tui.help.multi")
		}
	case scrPersona:
		fr.subtitle = f("tui.persona.title")
		fr.body, fr.focus = m.personaBody()
	case scrGentleKiro:
		fr.subtitle = f("tui.gentle-kiro.title")
		fr.body, fr.focus = m.gentleKiroBody()
	case scrPlan:
		fr.subtitle, fr.help = f("tui.plan.title"), f("tui.help.plan")
		fr.body, fr.focus = m.planBody()
	case scrApply:
		fr.subtitle, fr.help = f("tui.apply.title"), f("tui.help.apply")
		switch {
		case m.retiring:
			fr.help = f("tui.help.retiring")
		case m.cancelling:
			fr.help = f("tui.help.cancelling")
		}
		for _, s := range m.steps {
			fr.body = append(fr.body, m.st.line(s.sym, s.text))
		}
	case scrRetire:
		fr.subtitle = f("tui.apply.title")
		fr.body, fr.focus = m.retireBody()
	case scrResult:
		fr.subtitle, fr.help = f("tui.result.title"), f("tui.help.done")
		for _, l := range m.resultLines() {
			fr.body = append(fr.body, m.styleLine(l))
		}
	}
	return fr
}

// styleLine colors a leading state symbol; the symbol stays in the text.
func (m *Model) styleLine(l string) string {
	for _, sym := range []string{ui.SymOK, ui.SymWarn, ui.SymTodo, ui.SymError} {
		if rest, ok := strings.CutPrefix(l, sym+" "); ok {
			return m.st.line(sym, rest)
		}
	}
	return l
}

func (m *Model) section(title string) string { return m.st.section.Render(title) }

// menu renders the items with the cursor glyph and returns the cursor line.
func (m *Model) menu(items []menuItem) (lines []string, focus int) {
	for i, it := range items {
		if i == m.cur {
			focus = len(lines)
			lines = append(lines, m.st.cursor.Render("❯ "+it.label))
		} else {
			lines = append(lines, "  "+it.label)
		}
		if it.desc != "" { // wrapped here so every continuation line keeps the indent
			for _, l := range strings.Split(lipgloss.Wrap(it.desc, max(10, m.w-6), ""), "\n") {
				lines = append(lines, "    "+m.st.dim.Render(l))
			}
		}
	}
	return lines, focus
}

// compact drops the blank separators when the terminal is short, so the whole
// summary and the menu fit in 80x24.
func (m *Model) compact() bool { return m.h < 30 }

func (m *Model) homeBody() ([]string, int) {
	if m.inv.Version == 0 {
		return []string{m.msg.Format("tui.detecting")}, -1
	}
	var body []string
	for i, s := range ui.InventorySections(m.msg, m.inv) {
		if i > 0 && !m.compact() {
			body = append(body, "")
		}
		body = append(body, m.section(s.Title))
		for _, l := range s.Lines {
			body = append(body, "  "+m.st.line(l.Sym, l.Text))
		}
	}
	menu, focus := m.menu(m.items())
	if !m.compact() {
		body = append(body, "")
	}
	body = append(body, m.section(m.msg.Format("ask.action")))
	if !m.compact() {
		body = append(body, "")
	}
	return append(body, menu...), len(body) + focus
}

func (m *Model) detailsBody() []string {
	f := m.msg.Format
	var body []string
	for i, s := range ui.InventorySections(m.msg, m.inv) {
		if i > 0 {
			body = append(body, "")
		}
		body = append(body, m.section(s.Title))
		for _, l := range s.Lines {
			body = append(body, "  "+m.st.line(l.Sym, l.Text))
		}
		switch i {
		case 1:
			for _, r := range m.inv.Runtimes {
				if !r.Usable() && r.State != rt.StateDetectedHistory {
					body = append(body, "  "+m.st.line(ui.SymWarn, f("tui.details.runtime.absent", "name", r.Label)))
				}
			}
		case 2:
			if p := m.inv.Memory.ProjectMatch; p != "" {
				body = append(body, "  "+m.st.line(ui.SymOK, f("tui.details.engram.project", "detail", p)))
			}
			for _, w := range m.inv.Memory.Warnings {
				body = append(body, "  "+m.st.line(ui.SymWarn, f("warn."+w.Code, "detail", w.Detail)))
			}
		case 3:
			if c := m.inv.Other.GentleAI.Components; len(c) > 0 {
				body = append(body, "  "+m.st.line(" ", f("tui.details.gentle.components", "detail", strings.Join(c, ", "))))
			}
		}
	}
	return body
}

func (m *Model) memoryBody() ([]string, int) {
	var body []string
	body = append(body, m.section(m.msg.Format("tui.memory.title")), "")
	if secs := ui.InventorySections(m.msg, m.inv); len(secs) > 2 { // 0 system, 1 agents, 2 memory, 3 other
		for _, l := range secs[2].Lines {
			body = append(body, "  "+m.st.line(l.Sym, l.Text))
		}
	}
	body = append(body, "", m.msg.Format("tui.memory.intro"), "")
	menu, focus := m.menu(m.items())
	return append(body, menu...), len(body) + focus
}

func (m *Model) teamBody() ([]string, int) {
	f := m.msg.Format
	body := []string{m.section(f("tui.team.title")), ""}
	if m.inv.Server.Engine == "" {
		body = append(body, m.st.line(ui.SymWarn, f("tui.team.noengine.title")), f("tui.team.noengine.body"), "")
	} else {
		body = append(body, f("tui.team.intro"), "", m.st.line(ui.SymOK, f("tui.team.engine", "name", m.inv.Server.Engine)), f("tui.team.promise"), "")
	}
	menu, focus := m.menu(m.items())
	return append(body, menu...), len(body) + focus
}

func (m *Model) engramBody() ([]string, int) {
	f := m.msg.Format
	body := []string{m.section(f("tui.engram.title")), ""}
	if secs := ui.InventorySections(m.msg, m.inv); len(secs) > 2 {
		for _, l := range secs[2].Lines {
			body = append(body, "  "+m.st.line(l.Sym, l.Text))
		}
	}
	body = append(body, "", f("tui.engram.intro"), "")
	menu, focus := m.menu(m.items())
	return append(body, menu...), len(body) + focus
}

func (m *Model) personaBody() ([]string, int) {
	f := m.msg.Format
	var body []string
	runtime := ""
	if len(m.personaBlocks) > 0 {
		runtime = m.personaBlocks[0].Runtime
	}
	body = append(body, m.st.line(ui.SymWarn, f("ask.persona", "detail", runtime)), f("ask.persona.show"), "")
	for _, b := range m.personaBlocks {
		body = append(body, "  "+m.st.line(ui.SymTodo, b.Runtime+": "+m.cfg.Paths.Short(b.Path)+" ["+b.Target+"]"))
	}
	menu, focus := m.menu(m.items())
	body = append(body, "", f("ask.persona.confirm"), "")
	return append(body, menu...), len(body) + focus
}

func (m *Model) gentleKiroBody() ([]string, int) {
	f := m.msg.Format
	body := []string{m.st.line(ui.SymWarn, f("ask.gentle-kiro.title")), ""}
	if m.plan.GentleKiro != nil {
		for _, l := range ui.GentleKiroFacts(m.msg, *m.plan.GentleKiro) {
			body = append(body, l)
			if !m.compact() {
				body = append(body, "")
			}
		}
	}
	menu, focus := m.menu(m.items())
	body = append(body, f("ask.gentle-kiro.confirm"), "")
	return append(body, menu...), len(body) + focus
}

// planSymbols maps a plan verb to its state symbol (spec 50).
func planSymbol(k rt.Kind) string {
	switch k {
	case rt.Preserve, rt.NoAction:
		return ui.SymOK
	case rt.RemoveAfterVerify:
		return ui.SymWarn
	}
	return ui.SymTodo
}

func (m *Model) planBody() ([]string, int) {
	f := m.msg.Format
	var body []string
	if m.opts.Source == installer.SourceGentleAI {
		key, pairs := "ojo.migration.nomem", []string(nil)
		if n := m.inv.Memory.Observations; n > 0 {
			key, pairs = "ojo.migration", []string{"count", strconv.Itoa(n)}
		}
		body = append(body, m.st.line(ui.SymWarn, f("label.ojo")+": "+f(key, pairs...)), "")
	}
	for _, n := range m.plan.Notes {
		if n.Code == installer.NotePersonaConflict && m.personaAsked {
			continue // already asked and answered; the note only points at a CLI flag
		}
		if n.Code == installer.NoteGentleKiroPresent && m.gentleKiroAsked {
			continue // same for the Kiro question
		}
		body = append(body, m.st.line(ui.SymWarn, f("note."+n.Code, "detail", n.Detail)))
	}
	for _, w := range m.inv.Memory.Warnings {
		body = append(body, m.st.line(ui.SymWarn, f("warn."+w.Code, "detail", w.Detail)))
	}
	if len(body) > 0 && body[len(body)-1] != "" {
		body = append(body, "")
	}
	groups := ui.PlanGroups(m.msg, m.plan, m.cfg.Paths)
	if len(groups) == 0 || !m.plan.Mutates() {
		body = append(body, m.st.line(ui.SymOK, f("result.nothing")), "")
	}
	for _, g := range groups {
		body = append(body, m.st.line(planSymbol(g.Kind), fmt.Sprintf("%s (%d)", g.Label, len(g.Lines))))
	}
	if m.opts.RemoveGentleAI || len(m.plan.Retire) > 0 || m.plan.GentleArgs != nil {
		body = append(body, "", f("tui.plan.later"))
	}
	menu, focus := m.menu(m.items())
	body = append(body, "")
	at := len(body) + focus
	body = append(body, menu...)
	if m.showDetails {
		for _, g := range groups {
			body = append(body, "", m.section(g.Label))
			for _, l := range g.Lines {
				body = append(body, "  "+m.st.symbol(planSymbol(g.Kind))+" "+l)
			}
		}
	}
	return body, at
}

func (m *Model) retireBody() ([]string, int) {
	f := m.msg.Format
	body := []string{m.st.line(ui.SymOK, f("ask.retire.title")), ""}
	menu, focus := m.menu(m.items())
	at := len(body) + focus
	body = append(body, menu...)
	if m.retireHelp && m.retire != nil {
		body = append(body, "", m.st.line(ui.SymTodo, f("ask.retire.preview", "detail", strings.Join(m.retire.preview.Args, " "))))
	}
	return body, at
}
