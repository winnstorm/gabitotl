// Package skills embeds the agent skills GabitoTL installs into each runtime
// (<skills dir>/<name>/SKILL.md). The files are agent instructions, written in
// English; every one carries `metadata.author: gabitotl` so it is never mistaken
// for a skill of another harness (the Gentle AI retirement matches a different
// author). Installing them is the adapters' job; this package only supplies the
// content.
package skills

import (
	"embed"
	"io/fs"
	"sort"
)

//go:embed files/*/SKILL.md
var files embed.FS

// Author is the metadata.author marker every embedded skill carries.
const Author = "gabitotl"

// FileName is the name of the file inside each skill directory.
const FileName = "SKILL.md"

// Skill is one embedded skill: Name is also its directory name.
type Skill struct {
	Name    string
	Content string
}

// All returns every embedded skill, sorted by name.
func All() []Skill {
	entries, err := fs.ReadDir(files, "files")
	if err != nil {
		return nil
	}
	out := make([]Skill, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := files.ReadFile("files/" + e.Name() + "/" + FileName)
		if err != nil {
			continue
		}
		out = append(out, Skill{Name: e.Name(), Content: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Names lists the embedded skill names, sorted.
func Names() []string {
	all := All()
	out := make([]string, len(all))
	for i, s := range all {
		out[i] = s.Name
	}
	return out
}
