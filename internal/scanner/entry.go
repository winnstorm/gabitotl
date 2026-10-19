package scanner

import (
	"fmt"
	"strings"

	"github.com/winnstorm/gabitotl/internal/budget"
	"github.com/winnstorm/gabitotl/internal/memory"
)

// ProjectMapTopicKey makes saving the map an upsert in providers that support
// topic keys, so a re-scan refreshes the same record.
const ProjectMapTopicKey = "project/map"

// MapTokenBudget is the target size of the rendered map, in estimated tokens.
const MapTokenBudget = 800

// capLevels progressively shrink the list sections until the map fits the
// budget: modules, commands, history, other lists, architecture.
var capLevels = []struct{ mods, cmds, hist, other, arch int }{
	{15, 20, 5, 1 << 30, 1 << 30},
	{8, 10, 3, 4, 6},
	{4, 5, 0, 2, 3},
	{2, 3, 0, 1, 2},
}

// ToEntry renders the map as the project-map memory Entry, capped at about
// MapTokenBudget tokens (what was dropped is stated). project is the memory
// project key (empty leaves the provider default).
func (pm *ProjectMap) ToEntry(project string) memory.Entry {
	var content string
	for i, lv := range capLevels {
		content = pm.render(lv.mods, lv.cmds, lv.hist, lv.other, lv.arch, i > 0)
		if budget.EstimateTextTokens(content) <= MapTokenBudget {
			break
		}
	}
	if budget.EstimateTextTokens(content) > MapTokenBudget {
		r := []rune(content)
		max := int(float64(MapTokenBudget) * 3.6)
		if len(r) > max {
			content = strings.TrimSpace(string(r[:max-40])) + "\n... (map cut to fit ~800 tokens)"
		}
	}
	return memory.Entry{
		Type:     memory.TypeProjectMap,
		Title:    "Project map: " + pm.Name,
		Content:  strings.TrimSpace(content),
		Project:  project,
		TopicKey: ProjectMapTopicKey,
		Source:   "scanner",
	}
}

func (pm *ProjectMap) render(mods, cmds, hist, other, arch int, shrunk bool) string {
	var b strings.Builder
	list := func(title string, items []string, n int) {
		if len(items) == 0 || n == 0 {
			return
		}
		fmt.Fprintf(&b, "**%s**:\n", title)
		for _, it := range capList(items, n) {
			fmt.Fprintf(&b, "- %s\n", it)
		}
		b.WriteString("\n")
	}
	purpose := pm.Purpose
	if purpose == "" {
		purpose = "unknown (not inferable)"
	}
	fmt.Fprintf(&b, "**Purpose (guess)**: %s\n\n", purpose)
	list("Stack", pm.Stack, other)
	list("Architecture observed", pm.Architecture, arch)
	var ms []string
	for _, m := range pm.Modules {
		ms = append(ms, fmt.Sprintf("%s (%s, %d source files)", m.Path, m.Size, m.Files))
	}
	list("Main modules", ms, mods)
	var cs []string
	for _, c := range pm.Commands {
		cs = append(cs, fmt.Sprintf("`%s` (%s)", c.Command, c.Source))
	}
	list("Known commands", cs, cmds)
	list("Conventions", pm.Conventions, other)
	list("Integrations", pm.Integrations, other)
	list("Risks / delicate points", pm.Risks, other)
	var gs []string
	for _, c := range pm.Git.Commits {
		gs = append(gs, fmt.Sprintf("%s %s %s", c.Hash, c.Date, c.Subject))
	}
	list("Recent history", gs, min(hist, 5))
	list("Open questions", pm.OpenQuestions, other)
	if pm.Truncated {
		var ds []string
		for _, d := range pm.TopDirs {
			ds = append(ds, fmt.Sprintf("%s (%d files)", d.Name, d.Files))
		}
		list("Largest top-level directories", ds, 8)
	}
	fmt.Fprintf(&b, "**Scan**: %d files", pm.FilesVisited)
	if pm.Truncated {
		fmt.Fprintf(&b, " (truncated: first %d analysed in detail, the rest only counted per directory and type)", pm.DetailFiles)
	}
	if shrunk {
		b.WriteString("; lists shortened to fit ~800 tokens, run `gabitotl scan --json` for everything")
	}
	return b.String()
}

func capList(l []string, n int) []string {
	if len(l) <= n {
		return l
	}
	out := append([]string(nil), l[:n]...)
	return append(out, fmt.Sprintf("... and %d more", len(l)-n))
}
