package ui

import (
	"fmt"
	"io"
	"strconv"

	"github.com/winnstorm/gabitotl/internal/installer"
)

// maxListed bounds how many files a result lists before summarizing the rest.
const maxListed = 5

// GentleKiroFacts is what the question about Gentle AI's files in Kiro shows,
// shared by the plain installer and the full-screen one: what was found, what
// happens on yes and what stays. Lines carry no state symbol.
func GentleKiroFacts(m Messages, gk installer.GentleKiroInventory) []string {
	c := gk.Counts()
	yesNo := "ask.gentle-kiro.no"
	if c.Steering > 0 {
		yesNo = "ask.gentle-kiro.yes"
	}
	out := []string{
		m.Format("ask.gentle-kiro.found", "steering", m.Format(yesNo), "skills", strconv.Itoa(c.Skills),
			"agents", strconv.Itoa(c.Agents), "shared", strconv.Itoa(c.Shared)),
		m.Format("ask.gentle-kiro.moves"),
		m.Format("ask.gentle-kiro.stays"),
	}
	if c.Kept > 0 {
		out = append(out, m.Format("ask.gentle-kiro.kept", "kept", strconv.Itoa(c.Kept)))
	}
	return out
}

// ConfirmGentleKiro asks whether Gentle AI's files in Kiro should be moved to a
// backup. The answer is no unless the user types yes: keeping both is the safe
// choice and nothing is retired silently.
func (p *Prompter) ConfirmGentleKiro(gk installer.GentleKiroInventory) bool {
	if gk.Empty() {
		return false
	}
	fmt.Fprintf(p.w, "\n%s %s\n", SymWarn, p.m.Format("ask.gentle-kiro.title"))
	for _, l := range GentleKiroFacts(p.m, gk) {
		fmt.Fprintf(p.w, "  %s\n", l)
	}
	return p.Confirm(p.m.Format("ask.gentle-kiro.confirm"))
}

// renderGentleKiro prints what the retirement did: counts, the snapshot to
// restore from, and briefly the files that were not removed or are the user's.
func renderGentleKiro(w io.Writer, m Messages, g *installer.GentleKiroResult, paths Paths) {
	if g == nil {
		return
	}
	fmt.Fprintf(w, "\n%s\n", m.Format("result.gentle-kiro"))
	if len(g.Removed) > 0 {
		fmt.Fprintf(w, "%s %s\n", SymOK, m.Format("result.gentle-kiro.moved", "count", strconv.Itoa(len(g.Removed))))
	} else {
		fmt.Fprintf(w, "%s %s\n", SymWarn, m.Format("result.gentle-kiro.none"))
	}
	fmt.Fprintf(w, "%s %s\n", SymTodo, m.Format("result.gentle-kiro.restore", "id", g.Snapshot))
	listed := func(sym, key string, files []string) {
		if len(files) == 0 {
			return
		}
		fmt.Fprintf(w, "%s %s\n", sym, m.Format(key, "count", strconv.Itoa(len(files))))
		for i, f := range files {
			if i == maxListed {
				fmt.Fprintf(w, "    %s\n", m.Format("result.gentle-kiro.more", "count", strconv.Itoa(len(files)-maxListed)))
				break
			}
			fmt.Fprintf(w, "    %s\n", paths.Short(f))
		}
	}
	listed(SymWarn, "result.gentle-kiro.skipped", g.Skipped)
	listed(SymOK, "result.gentle-kiro.kept", g.Kept)
}
