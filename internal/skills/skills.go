// Package skills embeds the agent skills GabitoTL installs into each runtime
// (<skills dir>/<name>/SKILL.md, plus optional <name>/references/**). The files are agent instructions, written in
// English; every one carries `metadata.author: gabitotl` so it is never mistaken
// for a skill of another harness (the Gentle AI retirement matches a different
// author). Installing them is the adapters' job; this package only supplies the
// content.
package skills

import (
	"embed"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed files
var files embed.FS

// Author is the metadata.author marker every embedded skill carries.
const Author = "gabitotl"

// FileName is the name of the file inside each skill directory.
const FileName = "SKILL.md"

// Skill is one embedded skill: Name is also its directory name. Files holds
// every other file of the directory (references/...), keyed by its path relative
// to the skill directory with forward slashes. Optional skills are installed
// only when the person selects them (frontmatter `metadata: optional: "true"`).
type Skill struct {
	Name     string
	Content  string
	Files    map[string]string
	Optional bool
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
		root := "files/" + e.Name()
		b, err := files.ReadFile(root + "/" + FileName)
		if err != nil {
			continue
		}
		sk := Skill{Name: e.Name(), Content: string(b), Optional: isOptional(string(b))}
		_ = fs.WalkDir(files, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel := strings.TrimPrefix(p, root+"/")
			if rel == FileName {
				return nil
			}
			data, err := files.ReadFile(path.Clean(p))
			if err != nil {
				return nil
			}
			if sk.Files == nil {
				sk.Files = map[string]string{}
			}
			sk.Files[rel] = string(data)
			return nil
		})
		out = append(out, sk)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// isOptional reads `optional: "true"` from the metadata block of a SKILL.md.
func isOptional(content string) bool {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) < 2 || lines[0] != "---" {
		return false
	}
	inMeta := false
	for _, ln := range lines[1:] {
		if ln == "---" {
			break
		}
		if ln != "" && ln[0] != ' ' && ln[0] != '\t' {
			inMeta = strings.HasPrefix(ln, "metadata:")
			continue
		}
		if v, ok := strings.CutPrefix(strings.TrimSpace(ln), "optional:"); ok && inMeta {
			return strings.Trim(strings.TrimSpace(v), `"'`) == "true"
		}
	}
	return false
}

// Required returns the skills every install gets; Optional ones are chosen.
func Required() []Skill {
	var out []Skill
	for _, s := range All() {
		if !s.Optional {
			out = append(out, s)
		}
	}
	return out
}

// OptionalNames lists the optional skill names, sorted.
func OptionalNames() []string {
	var out []string
	for _, s := range All() {
		if s.Optional {
			out = append(out, s.Name)
		}
	}
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
