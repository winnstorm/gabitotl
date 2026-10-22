package installer

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Other agent systems GabitoTL can run next to. Detection is read-only: it looks
// for binaries on PATH, directories, files and a marker line inside the global or
// project instruction files, and reports where it looked so the UI can show it.

// Signal kinds.
const (
	SigBinary = "binary" // a command on PATH
	SigDir    = "dir"
	SigFile   = "file"
	SigMarker = "marker" // a text marker inside one of Files
	SigGlob   = "glob"   // a path pattern (weak signals only)
	SigAllDir = "dirs"   // every directory of Files exists (together)
)

// Conflict levels and what GabitoTL does about them.
const (
	ConflictLow    = "low"
	ConflictMedium = "medium"
	ConflictHigh   = "high"

	ActionCoexist = "coexist" // GabitoTL keeps its own blocks and never edits the system's files
	ActionReview  = "review"  // coexists, but the system also writes under the same agent folders
	ActionDecide  = "decide"  // Gentle AI: the migrate / keep decision
)

// markerScanLimit bounds how much of an instruction file the marker scan reads.
const markerScanLimit = 1 << 20

// SystemSignal is one thing to look for. Target is a binary name or a path
// relative to the home (global) or the project root (project); "~opencode/" is
// the OpenCode global config folder.
type SystemSignal struct {
	Kind   string
	Target string
	Strong bool
	Marker string   // SigMarker
	Files  []string // SigMarker: files to scan; SigAllDir: the directories
}

type systemDef struct {
	ID, Name, URL, Conflict, Action string
	Global, Project                 []SystemSignal
}

const openspecMarker = "<!-- OPENSPEC:START -->"

var systemRegistry = []systemDef{
	{ID: "gentle-ai", Name: "Gentle AI", URL: "https://github.com/Gentleman-Programming/gentle-ai", Conflict: ConflictHigh, Action: ActionDecide,
		Global: []SystemSignal{
			{Kind: SigBinary, Target: "gentle-ai", Strong: true},
			{Kind: SigDir, Target: ".gentle-ai", Strong: true},
			{Kind: SigFile, Target: ".gentle-ai/state.json", Strong: true},
		}},
	{ID: "superclaude", Name: "SuperClaude Framework", URL: "https://github.com/SuperClaude-Org/SuperClaude_Framework", Conflict: ConflictMedium, Action: ActionReview,
		Global: []SystemSignal{
			{Kind: SigBinary, Target: "superclaude", Strong: true},
			{Kind: SigDir, Target: ".claude/commands/sc", Strong: true},
		}},
	{ID: "spec-kit", Name: "Spec Kit", URL: "https://github.com/github/spec-kit", Conflict: ConflictLow, Action: ActionCoexist,
		Global: []SystemSignal{{Kind: SigBinary, Target: "specify", Strong: true}},
		Project: []SystemSignal{
			{Kind: SigDir, Target: ".specify/scripts", Strong: true},
			{Kind: SigGlob, Target: ".claude/skills/speckit-*"},
		}},
	{ID: "openspec", Name: "OpenSpec", URL: "https://github.com/Fission-AI/OpenSpec", Conflict: ConflictLow, Action: ActionCoexist,
		Global: []SystemSignal{
			{Kind: SigBinary, Target: "openspec", Strong: true},
			{Kind: SigMarker, Target: "instructions", Marker: openspecMarker, Strong: true, Files: []string{".claude/CLAUDE.md", "~opencode/AGENTS.md"}},
		},
		Project: []SystemSignal{
			{Kind: SigDir, Target: "openspec", Strong: true},
			{Kind: SigMarker, Target: "instructions", Marker: openspecMarker, Strong: true, Files: []string{"AGENTS.md", "CLAUDE.md"}},
			{Kind: SigGlob, Target: ".claude/skills/openspec-*"},
			{Kind: SigDir, Target: ".claude/commands/opsx"},
		}},
	{ID: "bmad", Name: "BMAD Method", URL: "https://github.com/bmad-code-org/BMAD-METHOD", Conflict: ConflictLow, Action: ActionCoexist,
		Project: []SystemSignal{
			{Kind: SigDir, Target: "_bmad", Strong: true},
			{Kind: SigDir, Target: ".bmad-core"},
			{Kind: SigGlob, Target: ".claude/skills/bmad*"},
		}},
	{ID: "task-master", Name: "Task Master", URL: "https://github.com/eyaltoledano/claude-task-master", Conflict: ConflictLow, Action: ActionCoexist,
		Global:  []SystemSignal{{Kind: SigBinary, Target: "task-master", Strong: true}},
		Project: []SystemSignal{{Kind: SigDir, Target: ".taskmaster", Strong: true}}},
	{ID: "ruflo", Name: "Ruflo (claude-flow)", URL: "https://github.com/ruvnet/ruflo", Conflict: ConflictLow, Action: ActionCoexist,
		Global: []SystemSignal{
			{Kind: SigBinary, Target: "ruflo", Strong: true},
			{Kind: SigBinary, Target: "claude-flow", Strong: true},
		},
		Project: []SystemSignal{{Kind: SigDir, Target: ".claude-flow", Strong: true}}},
	{ID: "omo", Name: "oh-my-opencode (omo)", URL: "https://github.com/code-yeongyu/oh-my-opencode", Conflict: ConflictLow, Action: ActionCoexist,
		Global: []SystemSignal{
			{Kind: SigFile, Target: ".omo/install.json", Strong: true},
			{Kind: SigFile, Target: ".omo/omo.jsonc", Strong: true},
			{Kind: SigBinary, Target: "omo", Strong: true},
		},
		Project: []SystemSignal{{Kind: SigFile, Target: ".omo/omo.jsonc", Strong: true}}},
	{ID: "ccpm", Name: "Claude Code PM (ccpm)", URL: "https://github.com/automazeio/ccpm", Conflict: ConflictLow, Action: ActionCoexist,
		Project: []SystemSignal{{Kind: SigAllDir, Target: ".claude/prds + .claude/epics", Files: []string{".claude/prds", ".claude/epics"}}}},
}

// SignalResult is one signal as checked: where it looked and whether it hit.
type SignalResult struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`         // binary name or path pattern
	Path   string `json:"path,omitempty"` // where it was found (binary) or looked (paths)
	Marker string `json:"marker,omitempty"`
	Strong bool   `json:"strong"`
	Found  bool   `json:"found"`
}

// SystemStatus is one other agent system as found (or not) on this machine.
type SystemStatus struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	URL      string         `json:"url"`
	Conflict string         `json:"conflict"`
	Action   string         `json:"action"`
	Found    bool           `json:"found"`    // a strong signal hit
	Possible bool           `json:"possible"` // only weak signals hit
	Signals  []SignalResult `json:"signals"`
}

// DetectSystems checks every system against the machine-wide signals.
func DetectSystems(env Env) []SystemStatus {
	return detectSystems(env, "", func(d systemDef) []SystemSignal { return d.Global })
}

// DetectProjectSystems checks the project-level signals under root.
func DetectProjectSystems(env Env, root string) []SystemStatus {
	if root == "" {
		return nil
	}
	return detectSystems(env, root, func(d systemDef) []SystemSignal { return d.Project })
}

func detectSystems(env Env, root string, pick func(systemDef) []SystemSignal) []SystemStatus {
	out := []SystemStatus{}
	for _, d := range systemRegistry {
		sigs := pick(d)
		if len(sigs) == 0 {
			continue
		}
		st := SystemStatus{ID: d.ID, Name: d.Name, URL: d.URL, Conflict: d.Conflict, Action: d.Action, Signals: []SignalResult{}}
		for _, s := range sigs {
			r := checkSignal(env, root, s)
			st.Signals = append(st.Signals, r)
			if r.Found && r.Strong {
				st.Found = true
			} else if r.Found {
				st.Possible = true
			}
		}
		st.Possible = st.Possible && !st.Found
		out = append(out, st)
	}
	return out
}

// systemPath resolves a signal path: under the project root, or under the home.
func systemPath(env Env, root, rel string) string {
	if strings.HasPrefix(rel, "~opencode/") {
		dir := filepath.Join(env.Home, ".config", "opencode")
		if x := env.Var("XDG_CONFIG_HOME"); x != "" {
			dir = filepath.Join(x, "opencode")
		}
		return filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(rel, "~opencode/")))
	}
	base := root
	if base == "" {
		base = env.Home
	}
	return filepath.Join(base, filepath.FromSlash(rel))
}

func checkSignal(env Env, root string, s SystemSignal) SignalResult {
	r := SignalResult{Kind: s.Kind, Target: s.Target, Strong: s.Strong, Marker: s.Marker}
	switch s.Kind {
	case SigBinary:
		r.Path, r.Found = lookSystemBinary(env, s.Target)
	case SigDir:
		r.Path = systemPath(env, root, s.Target)
		r.Found = isDir(r.Path)
	case SigFile:
		r.Path = systemPath(env, root, s.Target)
		r.Found = isFile(r.Path)
	case SigGlob:
		r.Path = systemPath(env, root, s.Target)
		m, _ := filepath.Glob(r.Path)
		r.Found = len(m) > 0
	case SigAllDir:
		r.Found = true
		var looked []string
		for _, f := range s.Files {
			p := systemPath(env, root, f)
			looked = append(looked, p)
			r.Found = r.Found && isDir(p)
		}
		r.Path = strings.Join(looked, " + ")
	case SigMarker:
		var looked []string
		for _, f := range s.Files {
			p := systemPath(env, root, f)
			looked = append(looked, p)
			if !r.Found && fileHasMarker(p, s.Marker) {
				r.Found = true
			}
		}
		r.Path = strings.Join(looked, " | ")
	}
	return r
}

// lookSystemBinary resolves a command on PATH. exec.LookPath already honours
// PATHEXT on Windows; the explicit .cmd/.bat/.exe retry covers npm shims when
// the injected lookup is stricter.
func lookSystemBinary(env Env, name string) (string, bool) {
	if p, ok := env.Look(name); ok {
		return p, true
	}
	if env.GOOS == "windows" {
		for _, ext := range []string{".cmd", ".exe", ".bat"} {
			if p, ok := env.Look(name + ext); ok {
				return p, true
			}
		}
	}
	return "", false
}

// fileHasMarker reports whether the first markerScanLimit bytes of path hold marker.
func fileHasMarker(path, marker string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, markerScanLimit))
	return err == nil && strings.Contains(string(b), marker)
}
