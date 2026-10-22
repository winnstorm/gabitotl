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
			limit := 300
			if s.Optional {
				limit = 1025 // optional skills carry a longer trigger list (agentskills.io allows 1024)
			}
			if d == "" || len(d) >= limit {
				t.Errorf("description must be non-empty and under %d chars, got %d", limit, len(d))
			}
			if !s.Optional {
				if i := strings.Index(d, "Trigger:"); i < 1 {
					t.Errorf("description must say what the skill does and then carry a Trigger: clause: %q", d)
				} else if strings.Contains(d[i+len("Trigger:"):], "Trigger:") {
					t.Errorf("description must have exactly one Trigger: clause: %q", d)
				}
			}
			if top["license"] != "Apache-2.0" {
				t.Errorf("license = %q, want Apache-2.0", top["license"])
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

// TestDeliverySkillSavesCIMinutes guards the push-by-milestone rule (D51).
func TestDeliverySkillSavesCIMinutes(t *testing.T) {
	var content string
	for _, s := range All() {
		if s.Name == "gabito-entrega" {
			content = s.Content
		}
	}
	for _, rule := range []string{
		"## Save CI minutes: push by milestone (GitHub and GitLab)", "Do not push after every commit",
		"Ask the user first", "even when the saved automation is `all`", "Never push to \"see if CI passes\"",
		"On Bitbucket and other hosts the saved settings decide", "never apply unasked",
	} {
		if !strings.Contains(content, rule) {
			t.Errorf("gabito-entrega is missing %q", rule)
		}
	}
}

// TestTerminalSkillCarriesTheMovedPersonaRules guards the rules that left the
// always-on persona for the on-demand skill (D42): nothing may be lost.
func TestTerminalSkillCarriesTheMovedPersonaRules(t *testing.T) {
	var content string
	for _, s := range All() {
		if s.Name == "gabito-terminal" {
			content = s.Content
		}
	}
	for _, rule := range []string{
		"Python is the default", "PowerShell only for Windows-specific work", "avoid PowerShell one-liners",
		"Do not retry the same command", "`.gabitotl/tmp/<purpose>.py`", "`pathlib`", "subprocess.run([...]",
		"Python is missing", "Local permission does not authorize remote work", "explicit OK for the destination",
		"Never discover or reuse ambient SSH agents", "ask and\nstay local",
	} {
		if !strings.Contains(content, rule) {
			t.Errorf("gabito-terminal is missing %q", rule)
		}
	}
}

func TestIsOptionalReadsOnlyTheMetadataFlag(t *testing.T) {
	for content, want := range map[string]bool{
		"---\nname: a\nmetadata:\n  author: x\n  optional: \"true\"\n---\nbody\n": true,
		"---\nname: a\nmetadata:\n  optional: \"false\"\n---\n":                   false,
		"---\nname: a\noptional: \"true\"\n---\n":                                 false, // not under metadata
		"---\nname: a\nmetadata:\n  author: x\n---\noptional: \"true\"\n":         false, // body text
		"no front matter\n": false,
	} {
		if got := isOptional(content); got != want {
			t.Errorf("isOptional(%q) = %v, want %v", content, got, want)
		}
	}
}

func TestRequiredAndOptionalPartitionAll(t *testing.T) {
	if len(Required())+len(OptionalNames()) != len(All()) {
		t.Fatal("Required and Optional must partition All")
	}
}

func skillByName(t *testing.T, name string) Skill {
	t.Helper()
	for _, s := range All() {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("skill %s is not embedded", name)
	return Skill{}
}

func TestGabitoDesignIsOptionalAndWellFormed(t *testing.T) {
	s := skillByName(t, "gabito-design")
	if !s.Optional {
		t.Error("gabito-design must be optional")
	}
	top, meta, _ := frontMatter(t, s.Content)
	if top["name"] != "gabito-design" || top["license"] != "Apache-2.0" || meta["optional"] != "true" {
		t.Errorf("front matter: %v %v", top, meta)
	}
	if n := len(strings.Split(s.Content, "\n")); n > 300 {
		t.Errorf("SKILL.md has %d lines, want at most 300", n)
	}
	// Front matter is string-only for cross-agent safety: every metadata value is a scalar.
	for k, v := range meta {
		if strings.ContainsAny(v, "[{") {
			t.Errorf("metadata.%s must be a plain string, got %q", k, v)
		}
	}
	d := top["description"]
	for _, w := range []string{"docx", "pptx", "PDF", "xlsx", "diagrams", "logos", "critique", "audit", "polish", "layout"} {
		if !strings.Contains(d, w) {
			t.Errorf("description must name %q", w)
		}
	}
	for _, f := range []string{"commands.md", "modes.md", "web-ui.md", "documents.md", "presentations.md", "data-charts.md", "diagrams.md", "images.md", "anti-patterns.md", "design-md.md"} {
		if s.Files["references/"+f] == "" {
			t.Errorf("references/%s is missing or empty", f)
		}
	}
}

func TestEveryLinkedReferenceExists(t *testing.T) {
	for _, s := range All() {
		for _, line := range strings.Split(s.Content, "\n") {
			for _, part := range strings.Split(line, "`")[1:] {
				if !strings.HasPrefix(part, "references/") || !strings.HasSuffix(part, ".md") || strings.Contains(part, "<") {
					continue
				}
				if _, ok := s.Files[part]; !ok {
					t.Errorf("%s links %s, which is not embedded", s.Name, part)
				}
			}
		}
		// Every reference is linked from SKILL.md, so none is dead weight.
		for rel := range s.Files {
			if !strings.Contains(s.Content, "`"+rel+"`") {
				t.Errorf("%s: %s is not linked from SKILL.md", s.Name, rel)
			}
		}
	}
}

func TestOriginalWorkMentionsItsInspirationOnlyInTheCredit(t *testing.T) {
	const credit = "GabitoDesign is an original work inspired by Impeccable by Paul Bakaus (Apache-2.0, https://impeccable.style). No Impeccable text or code is included."
	s := skillByName(t, "gabito-design")
	all := map[string]string{"SKILL.md": s.Content}
	for rel, c := range s.Files {
		all[rel] = c
	}
	for name, c := range all {
		c = strings.ReplaceAll(c, credit, "")
		if strings.Contains(strings.ToLower(c), "impeccable") {
			t.Errorf("%s names the inspiration outside the credit line", name)
		}
	}
	if !strings.Contains(s.Content, credit) {
		t.Error("SKILL.md must carry the credit line")
	}
}

func TestEmbeddedSkillInventoryIsPinned(t *testing.T) {
	if _, err := load(); err != nil {
		t.Fatalf("embedded skills must load: %v", err)
	}
	if n := len(Required()); n != 8 {
		t.Fatalf("required skills = %d, want 8", n)
	}
	for _, s := range All() {
		if s.Name != "gabito-design" {
			continue
		}
		if got := 1 + len(s.Files); got != 11 {
			t.Fatalf("gabito-design files = %d (SKILL.md + references), want 11", got)
		}
		return
	}
	t.Fatal("gabito-design is not embedded")
}
