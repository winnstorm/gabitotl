package scanner

import (
	"fmt"
	"strings"

	"github.com/winnstorm/gabitotl/internal/memory"
)

// ProjectMapTopicKey makes saving the map an upsert in providers that support
// topic keys, so a re-scan refreshes the same record.
const ProjectMapTopicKey = "project/map"

// ToEntry renders the map as the project-map memory Entry. project is the
// memory project key (empty leaves the provider default).
func (pm *ProjectMap) ToEntry(project string) memory.Entry {
	var b strings.Builder
	list := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "**%s**:\n", title)
		for _, it := range items {
			fmt.Fprintf(&b, "- %s\n", it)
		}
		b.WriteString("\n")
	}
	purpose := pm.Purpose
	if purpose == "" {
		purpose = "unknown (not inferable)"
	}
	fmt.Fprintf(&b, "**Purpose (guess)**: %s\n\n", purpose)
	list("Stack", pm.Stack)
	list("Architecture observed", pm.Architecture)
	var mods []string
	for _, m := range pm.Modules {
		mods = append(mods, fmt.Sprintf("%s (%s, %d source files)", m.Path, m.Size, m.Files))
	}
	list("Main modules", capList(mods, 15))
	var cmds []string
	for _, c := range pm.Commands {
		cmds = append(cmds, fmt.Sprintf("`%s` (%s)", c.Command, c.Source))
	}
	list("Known commands", capList(cmds, 20))
	list("Conventions", pm.Conventions)
	list("Integrations", pm.Integrations)
	list("Risks / delicate points", pm.Risks)
	var gitl []string
	for _, c := range pm.Git.Commits {
		gitl = append(gitl, fmt.Sprintf("%s %s %s", c.Hash, c.Date, c.Subject))
	}
	list("Recent history", capList(gitl, 5))
	list("Open questions", pm.OpenQuestions)
	fmt.Fprintf(&b, "**Scan**: %d files visited", pm.FilesVisited)
	if pm.Truncated {
		b.WriteString(" (truncated at cap)")
	}
	return memory.Entry{
		Type:     memory.TypeProjectMap,
		Title:    "Project map: " + pm.Name,
		Content:  strings.TrimSpace(b.String()),
		Project:  project,
		TopicKey: ProjectMapTopicKey,
		Source:   "scanner",
	}
}

func capList(l []string, n int) []string {
	if len(l) <= n {
		return l
	}
	out := append([]string(nil), l[:n]...)
	return append(out, fmt.Sprintf("... and %d more", len(l)-n))
}
