package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// --- Gentle-AI ------------------------------------------------------------

type gentleAISource struct{}

func (gentleAISource) Name() string { return "gentle-ai" }

func gentleHome(env Env) string { return filepath.Join(env.Home, ".gentle-ai") }

func (gentleAISource) Detect(env Env) Detection {
	bin, hasBin := env.look("gentle-ai")
	state := filepath.Join(gentleHome(env), "state.json")
	hasState, hasHome := exists(state), exists(gentleHome(env))
	hasODD := env.ProjectRoot != "" && exists(filepath.Join(env.ProjectRoot, "odd", "tasks"))
	return Detection{Source: "gentle-ai", Found: hasBin || hasState || hasHome || hasODD, Signals: []Signal{
		{Name: "binary gentle-ai", Found: hasBin, Detail: bin},
		{Name: "state ~/.gentle-ai/state.json", Found: hasState},
		{Name: "home ~/.gentle-ai", Found: hasHome},
		{Name: "project odd/tasks", Found: hasODD},
	}}
}

// names extracts names from a JSON list of strings or of objects with a
// name/id field, tolerating the unknown real schema of state.json.
func names(v any) []string {
	var out []string
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			switch x := e.(type) {
			case string:
				out = append(out, x)
			case map[string]any:
				for _, k := range []string{"name", "id", "agent"} {
					if s, ok := x[k].(string); ok {
						out = append(out, s)
						break
					}
				}
			}
		}
	case map[string]any:
		for k := range t {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func (gentleAISource) Collect(_ context.Context, env Env) (Collected, error) {
	var c Collected
	if b, err := os.ReadFile(filepath.Join(gentleHome(env), "state.json")); err == nil {
		var st map[string]any
		if err := json.Unmarshal(b, &st); err != nil {
			c.Skipped = append(c.Skipped, "~/.gentle-ai/state.json: not valid JSON")
		} else {
			agents := names(st["installed_agents"])
			comps := names(st["components"])
			ver, _ := st["version"].(string)
			body := fmt.Sprintf("Gentle-AI %s manages agents: %s. Components: %s. State schema keys are not copied.", orNone(ver), joinOr(agents), joinOr(comps))
			c.Items = append(c.Items, Knowledge{Kind: KindTooling, Title: "Gentle-AI installation", Body: body, Origin: "gentle-ai", Path: "~/.gentle-ai/state.json"})
		}
	}
	// Managed blocks Gentle-AI left in project instruction files (names only).
	for _, f := range instructionFiles {
		text, _, ok := readText(filepath.Join(env.ProjectRoot, f))
		if !ok {
			continue
		}
		var blocks []string
		for _, m := range gentleStart.FindAllStringSubmatch(text, -1) {
			blocks = append(blocks, m[1])
		}
		if len(blocks) > 0 {
			c.Items = append(c.Items, Knowledge{Kind: KindTooling, Title: "Gentle-AI managed blocks in " + filepath.ToSlash(f),
				Body: "Managed blocks present: " + strings.Join(blocks, ", ") + ". They are not imported; retire them with the persona migration.", Origin: "gentle-ai", Path: filepath.ToSlash(f)})
		}
	}
	// ODD task documents carry feature state worth keeping.
	for _, f := range listFiles(filepath.Join(env.ProjectRoot, "odd", "tasks"), ".md") {
		text, why, ok := readText(f)
		if !ok {
			c.Skipped = append(c.Skipped, rel(env.ProjectRoot, f)+": "+why)
			continue
		}
		var open []string
		for _, l := range strings.Split(text, "\n") {
			if t := strings.TrimSpace(l); strings.HasPrefix(t, "- [ ]") && len(open) < 10 {
				open = append(open, strings.TrimSpace(strings.TrimPrefix(t, "- [ ]")))
			}
		}
		body := "Open items: " + joinOr(open)
		if len(open) == 0 {
			body = "No open items recorded."
		}
		c.Items = append(c.Items, Knowledge{Kind: KindTask, Title: "ODD task document: " + baseName(f), Body: excerpt(body, maxBodyChars), Origin: "gentle-ai", Path: rel(env.ProjectRoot, f)})
	}
	return c, nil
}

func orNone(s string) string {
	if s == "" {
		return "(version unknown)"
	}
	return s
}

// --- Engram ---------------------------------------------------------------

type engramSource struct{}

func (engramSource) Name() string { return "engram" }

func engramDir(env Env) string {
	if d := env.getenv("ENGRAM_DATA_DIR"); d != "" {
		return d
	}
	return filepath.Join(env.Home, ".engram")
}

func (engramSource) Detect(env Env) Detection {
	bin, hasBin := env.look("engram")
	dir := engramDir(env)
	hasDir := exists(dir)
	hasDB := exists(filepath.Join(dir, "engram.db"))
	hasProj := env.ProjectRoot != "" && exists(filepath.Join(env.ProjectRoot, ".engram"))
	return Detection{Source: "engram", Found: hasBin || hasDir || hasProj, Signals: []Signal{
		{Name: "binary engram", Found: hasBin, Detail: bin},
		{Name: "data dir " + dir, Found: hasDir},
		{Name: "database engram.db", Found: hasDB},
		{Name: "project .engram/", Found: hasProj},
	}}
}

// Collect never opens the Engram database: it reports what exists and, through
// the injected CLI adapter, the observation count of the project. The memory
// itself stays in Engram and is read through memory.Provider.
func (engramSource) Collect(ctx context.Context, env Env) (Collected, error) {
	var c Collected
	dir := engramDir(env)
	var parts []string
	parts = append(parts, "Engram data directory: "+dir)
	if exists(filepath.Join(dir, "engram.db")) {
		parts = append(parts, "database present (reused, never copied)")
	}
	key := env.ProjectKey
	if env.ProjectRoot != "" {
		for _, f := range []string{"config.json", "manifest.json"} {
			if b, err := os.ReadFile(filepath.Join(env.ProjectRoot, ".engram", f)); err == nil {
				var m map[string]any
				if json.Unmarshal(b, &m) == nil {
					parts = append(parts, ".engram/"+f+" present")
					if p, ok := m["project"].(string); ok && key == "" {
						key = p
					}
				}
			}
		}
	}
	if key != "" {
		parts = append(parts, "project: "+key)
	}
	if env.Projects != nil {
		ps, err := env.Projects(ctx)
		switch {
		case err != nil:
			c.Skipped = append(c.Skipped, "engram projects list: "+err.Error())
		default:
			for _, p := range ps {
				if p.Name == key {
					parts = append(parts, fmt.Sprintf("%d observations", p.Observations))
				}
			}
		}
	}
	c.Items = append(c.Items, Knowledge{Kind: KindTooling, Title: "Engram memory for this project", Body: strings.Join(parts, "; ") + ". Query it through the memory provider instead of re-documenting it.", Origin: "engram", Path: ".engram"})
	return c, nil
}
