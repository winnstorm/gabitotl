package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/winnstorm/gabitotl/internal/installer/ui"
)

// styles holds every Lip Gloss style. With colors off (NO_COLOR) all styles are
// empty, so Render returns the text untouched: meaning never depends on color,
// the state symbols and the cursor glyph carry it.
type styles struct {
	color                                 bool
	title, subtitle, section, dim, cursor lipgloss.Style
	ok, warn, todo, err                   lipgloss.Style
}

func newStyles(colors bool) styles {
	s := styles{color: colors}
	if !colors {
		return s
	}
	fg := func(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }
	s.title = lipgloss.NewStyle().Bold(true)
	s.subtitle = fg("8")
	s.section = lipgloss.NewStyle().Bold(true)
	s.dim = fg("8")
	s.cursor = fg("6").Bold(true)
	s.ok, s.warn, s.todo, s.err = fg("2"), fg("3"), fg("6"), fg("1")
	return s
}

// symbol colors a state symbol; the symbol itself is always printed.
func (s styles) symbol(sym string) string {
	switch sym {
	case ui.SymOK:
		return s.ok.Render(sym)
	case ui.SymWarn:
		return s.warn.Render(sym)
	case ui.SymTodo:
		return s.todo.Render(sym)
	case ui.SymError:
		return s.err.Render(sym)
	}
	return sym
}

// line renders "<symbol> <text>" with the symbol colored.
func (s styles) line(sym, text string) string { return s.symbol(sym) + " " + text }

// header is the screen title: a rounded box when the terminal is roomy, a
// single line otherwise.
func (s styles) header(title, subtitle string, w, h int) []string {
	if w >= 44 && h >= 30 {
		box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Width(40).Align(lipgloss.Center)
		if s.color {
			box = box.BorderForeground(lipgloss.Color("8"))
		}
		return strings.Split(box.Render(s.title.Render(title)+"\n"+s.subtitle.Render(subtitle)), "\n")
	}
	return []string{s.title.Render(title) + " " + s.subtitle.Render("· "+subtitle)}
}
