package skills

import (
	"strings"
	"testing"
)

// frontMatter splits a SKILL.md into its top-level keys, the metadata keys and the body.
func frontMatter(t *testing.T, content string) (top, meta map[string]string, body string) {
	t.Helper()
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) < 3 || lines[0] != "---" {
		t.Fatalf("missing front matter opening")
	}
	top, meta = map[string]string{}, map[string]string{}
	end, inMeta := -1, false
	for i := 1; i < len(lines); i++ {
		ln := lines[i]
		if ln == "---" {
			end = i
			break
		}
		if strings.TrimSpace(ln) == "" {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimSpace(ln), ":")
		if !ok {
			t.Fatalf("front matter line without a colon: %q", ln)
		}
		v = strings.Trim(strings.TrimSpace(v), `"`)
		if ln[0] == ' ' || ln[0] == '\t' {
			if !inMeta {
				t.Fatalf("indented line outside metadata: %q", ln)
			}
			meta[k] = v
			continue
		}
		inMeta = k == "metadata"
		top[k] = v
	}
	if end < 0 {
		t.Fatal("front matter is not closed")
	}
	return top, meta, strings.Join(lines[end+1:], "\n")
}

func TestEmbeddedSkillsAreValid(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("no skills embedded")
	}
	for _, s := range all {
		t.Run(s.Name, func(t *testing.T) {
			top, meta, body := frontMatter(t, s.Content)
			if top["name"] != s.Name {
				t.Errorf("name %q must equal the directory %q", top["name"], s.Name)
			}
			d := top["description"]
			if d == "" || len(d) >= 300 {
				t.Errorf("description must be non-empty and under 300 chars, got %d", len(d))
			}
			if i := strings.Index(d, "Trigger:"); i < 1 {
				t.Errorf("description must say what the skill does and then carry a Trigger: clause: %q", d)
			} else if strings.Contains(d[i+len("Trigger:"):], "Trigger:") {
				t.Errorf("description must have exactly one Trigger: clause: %q", d)
			}
			if top["license"] != "MIT" {
				t.Errorf("license = %q, want MIT", top["license"])
			}
			if meta["author"] != Author {
				t.Errorf("metadata.author = %q, want %q", meta["author"], Author)
			}
			if meta["version"] == "" {
				t.Error("metadata.version is required")
			}
			if strings.TrimSpace(body) == "" {
				t.Error("body is empty")
			}
		})
	}
}

func TestAllIsSortedAndNamesMatch(t *testing.T) {
	all, names := All(), Names()
	if len(all) != len(names) {
		t.Fatalf("%d skills, %d names", len(all), len(names))
	}
	for i := range all {
		if all[i].Name != names[i] {
			t.Errorf("names[%d] = %q, want %q", i, names[i], all[i].Name)
		}
		if i > 0 && all[i-1].Name >= all[i].Name {
			t.Errorf("not sorted: %q before %q", all[i-1].Name, all[i].Name)
		}
	}
	found := false
	for _, n := range names {
		found = found || n == "gabito-terminal"
	}
	if !found {
		t.Error("gabito-terminal must ship")
	}
}
