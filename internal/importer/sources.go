package importer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/ownership"
)

var (
	reDecision   = regexp.MustCompile(`(?i)\b(decision|decisi[oó]n|adr|we chose|chosen)\b`)
	reConstraint = regexp.MustCompile(`(?i)\b(constraint|must not|never|do not|don't|restriction|security|rules?)\b`)
	reCommand    = regexp.MustCompile(`(?i)\b(commands?|build|tests?|scripts?|usage|how to run|make)\b`)
	reArch       = regexp.MustCompile(`(?i)\b(architecture|arquitectura|design|structure|layers?|modules?|stack)\b`)
	reTask       = regexp.MustCompile(`(?i)\b(todo|tasks?|pending|status|roadmap|backlog)\b`)
)

// classify maps a heading/body to a Kind with simple keyword heuristics.
func classify(title string) Kind {
	switch {
	case reDecision.MatchString(title):
		return KindDecision
	case reConstraint.MatchString(title):
		return KindConstraint
	case reCommand.MatchString(title):
		return KindCommand
	case reArch.MatchString(title):
		return KindArchitecture
	case reTask.MatchString(title):
		return KindTask
	}
	return KindConvention
}

func stripFront(s string) string {
	if d, err := gabitotl.ParseDoc([]byte(s)); err == nil && d.HasFront {
		return d.Body
	}
	return s
}

func firstHeading(s, fallback string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "#") {
			if h := strings.TrimSpace(strings.TrimLeft(t, "#")); h != "" {
				return h
			}
		}
	}
	return fallback
}

func baseName(p string) string { return strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)) }

func listFiles(dir, ext string) []string {
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range es {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func listDirs(dir string) []string {
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range es {
		if e.IsDir() {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// --- Kiro -----------------------------------------------------------------

type kiroSource struct{}

func (kiroSource) Name() string { return "kiro" }

var kiroDirs = []string{"steering", "specs", "settings", "agents", "skills"}

func (kiroSource) Detect(env Env) Detection {
	d := Detection{Source: "kiro"}
	for _, sub := range kiroDirs {
		p := filepath.Join(env.ProjectRoot, ".kiro", sub)
		f := exists(p)
		d.Found = d.Found || f
		d.Signals = append(d.Signals, Signal{Name: ".kiro/" + sub, Found: f})
	}
	return d
}

func (kiroSource) Collect(_ context.Context, env Env) (Collected, error) {
	var c Collected
	root := filepath.Join(env.ProjectRoot, ".kiro")
	add := func(path string, kind Kind, title string, forceKind bool) {
		text, why, ok := readText(path)
		if !ok {
			c.Skipped = append(c.Skipped, rel(env.ProjectRoot, path)+": "+why)
			return
		}
		body := strings.TrimSpace(stripFront(text))
		if body == "" {
			return
		}
		if t := firstHeading(body, ""); t != "" && title == "" {
			title = t
		}
		if title == "" {
			title = baseName(path)
		}
		if !forceKind {
			kind = classify(title)
		}
		c.Items = append(c.Items, Knowledge{Kind: kind, Title: title, Body: excerpt(body, maxBodyChars), Origin: "kiro", Path: rel(env.ProjectRoot, path)})
	}
	for _, f := range listFiles(filepath.Join(root, "steering"), ".md") {
		if filepath.Base(f) == "gabitotl.md" { // our own asset, not Kiro knowledge
			continue
		}
		kind, force := KindConvention, false
		switch baseName(f) {
		case "product", "tech", "structure":
			kind, force = KindArchitecture, true
		}
		add(f, kind, "steering: "+baseName(f), force)
	}
	specKinds := map[string]Kind{"requirements": KindConstraint, "design": KindArchitecture, "tasks": KindTask}
	for _, dir := range listDirs(filepath.Join(root, "specs")) {
		for name, kind := range specKinds {
			if p := filepath.Join(dir, name+".md"); exists(p) {
				add(p, kind, "spec "+filepath.Base(dir)+": "+name, true)
			}
		}
	}
	c.Items = append(c.Items, kiroTooling(env, root, &c)...)
	sort.SliceStable(c.Items, func(i, j int) bool { return c.Items[i].Path < c.Items[j].Path })
	return c, nil
}

// kiroTooling lists configured MCP servers (names only: never env, args or
// URLs, which can carry credentials), agents and skills.
func kiroTooling(env Env, root string, c *Collected) []Knowledge {
	var out []Knowledge
	if b, err := os.ReadFile(filepath.Join(root, "settings", "mcp.json")); err == nil {
		var cfg struct {
			Servers map[string]json.RawMessage `json:"mcpServers"`
		}
		if json.Unmarshal(b, &cfg) == nil && len(cfg.Servers) > 0 {
			var names []string
			for n := range cfg.Servers {
				names = append(names, n)
			}
			sort.Strings(names)
			out = append(out, Knowledge{Kind: KindTooling, Title: "Kiro MCP servers", Body: "Configured MCP servers (names only): " + strings.Join(names, ", "), Origin: "kiro", Path: ".kiro/settings/mcp.json"})
		}
	}
	var agents, skills []string
	for _, f := range append(listFiles(filepath.Join(root, "agents"), ".json"), listFiles(filepath.Join(root, "agents"), ".md")...) {
		agents = append(agents, baseName(f))
	}
	for _, d := range listDirs(filepath.Join(root, "skills")) {
		skills = append(skills, filepath.Base(d))
	}
	if len(agents)+len(skills) > 0 {
		sort.Strings(agents)
		out = append(out, Knowledge{Kind: KindTooling, Title: "Kiro agents and skills",
			Body: "Agents: " + joinOr(agents) + ". Skills: " + joinOr(skills) + ".", Origin: "kiro", Path: ".kiro/agents, .kiro/skills"})
	}
	return out
}

func joinOr(l []string) string {
	if len(l) == 0 {
		return "none"
	}
	return strings.Join(l, ", ")
}

// --- generic instruction files -------------------------------------------

type instructionsSource struct{}

func (instructionsSource) Name() string { return "instructions" }

var instructionFiles = []string{"CLAUDE.md", "AGENTS.md", filepath.Join(".claude", "CLAUDE.md")}

func (instructionsSource) Detect(env Env) Detection {
	d := Detection{Source: "instructions"}
	for _, f := range instructionFiles {
		ok := exists(filepath.Join(env.ProjectRoot, f))
		d.Found = d.Found || ok
		d.Signals = append(d.Signals, Signal{Name: f, Found: ok})
	}
	return d
}

var gentleStart = regexp.MustCompile(`<!--\s*gentle-ai:([A-Za-z0-9._-]+)\s*-->`)

const gentleEnd = `<!--\s*/gentle-ai:%s\s*-->`

// userContent removes GabitoTL sections and Gentle-AI managed blocks, leaving
// only instructions a person wrote.
func userContent(s string) string {
	for {
		var blocks []ownership.Block
		blocks = append(blocks, ownership.Sections(s)...)
		blocks = append(blocks, ownership.FindBlocks(s, gentleStart, gentleEnd)...)
		if len(blocks) == 0 {
			return s
		}
		b := blocks[0]
		for _, o := range blocks[1:] {
			if o.Start > b.Start {
				b = o
			}
		}
		s = s[:b.Start] + s[b.End:]
	}
}

func (instructionsSource) Collect(_ context.Context, env Env) (Collected, error) {
	var c Collected
	for _, f := range instructionFiles {
		path := filepath.Join(env.ProjectRoot, f)
		if !exists(path) {
			continue
		}
		text, why, ok := readText(path)
		if !ok {
			c.Skipped = append(c.Skipped, f+": "+why)
			continue
		}
		for _, sec := range splitSections(userContent(text), filepath.Base(f)) {
			if gabitotl.ValidateNoSecrets([]byte(sec.body)) != nil {
				c.Skipped = append(c.Skipped, f+" ("+sec.title+"): looks like it contains a secret")
				continue
			}
			if len([]rune(strings.TrimSpace(sec.body))) < 20 {
				continue
			}
			c.Items = append(c.Items, Knowledge{Kind: classify(sec.title), Title: sec.title, Body: excerpt(sec.body, maxBodyChars), Origin: "instructions", Path: filepath.ToSlash(f)})
		}
	}
	return c, nil
}

type section struct{ title, body string }

// splitSections splits Markdown on level 1-2 headings. Text before the first
// heading is titled after the file.
func splitSections(s, fallback string) []section {
	var out []section
	cur := section{title: fallback}
	var body []string
	flush := func() {
		cur.body = strings.TrimSpace(strings.Join(body, "\n"))
		if cur.body != "" {
			out = append(out, cur)
		}
		body = nil
	}
	inFence := false
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			inFence = !inFence
		}
		if !inFence && (strings.HasPrefix(l, "# ") || strings.HasPrefix(l, "## ")) {
			flush()
			cur = section{title: strings.TrimSpace(strings.TrimLeft(l, "#"))}
			continue
		}
		body = append(body, l)
	}
	flush()
	return out
}
