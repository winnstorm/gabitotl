// Package runtime is the core contract for coding-agent runtimes (installer
// spec sections 7 and 32). Core code only talks to the Adapter interface and
// iterates a Registry; every file format, path and binary name lives in the
// adapter sub-packages (claudecode, opencode, kiro). The composition root is
// package all.
package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/ownership"
)

// ErrUnsupported is returned for operations a runtime cannot do (for example
// ListModels on a runtime without a model catalog).
var ErrUnsupported = errors.New("runtime: operation not supported")

// Env carries every external dependency of an adapter. Tests inject temp
// directories and fakes; no adapter reads the process environment, the real
// home directory or PATH on its own.
type Env struct {
	Home        string // user home (production: os.UserHomeDir; Windows: %USERPROFILE%)
	ProjectRoot string // project root, "" when not inside a project
	StateDir    string // GabitoTL global state dir (production: <home>/.gabitotl)
	GOOS        string // runtime.GOOS
	Getenv      func(string) string
	LookPath    func(string) (string, error)
	Exists      func(string) bool // app/dir presence probe; nil uses os.Stat
	Now         func() time.Time
	Exe         string // absolute path of the gabitotl binary for host hooks; "" means rely on PATH
}

// ResolveExe returns the absolute, symlink-free path of the running binary, or
// "" when it cannot be resolved or lives in a temp or Go build directory (go
// run, tests), where the path would not outlive the process.
func ResolveExe() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil || !filepath.IsAbs(exe) {
		return ""
	}
	low := strings.ToLower(filepath.ToSlash(exe))
	tmp := strings.ToLower(filepath.ToSlash(os.TempDir()))
	if strings.Contains(low, "/go-build") || (tmp != "" && strings.HasPrefix(low, strings.TrimRight(tmp, "/")+"/")) {
		return ""
	}
	return exe
}

// Command returns the shell command that runs gabitotl with args from a host
// hook: the absolute binary path (double-quoted when it has spaces) or plain
// "gabitotl" when no path is known.
func (e Env) Command(args string) string {
	bin := e.Exe
	if bin == "" {
		bin = "gabitotl"
	} else if strings.ContainsAny(bin, " \t") {
		bin = `"` + bin + `"`
	}
	return bin + " " + args
}

// OSEnv builds the production Env for a project root.
func OSEnv(goos, projectRoot string) (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, err
	}
	return Env{
		Home: home, ProjectRoot: projectRoot, StateDir: filepath.Join(home, ".gabitotl"), GOOS: goos,
		Getenv: os.Getenv, LookPath: exec.LookPath, Now: time.Now, Exe: ResolveExe(),
	}, nil
}

// Var reads an environment variable through the injected lookup.
func (e Env) Var(k string) string {
	if e.Getenv == nil {
		return ""
	}
	return e.Getenv(k)
}

// Look reports whether a binary is on PATH.
func (e Env) Look(bin string) (string, bool) {
	if e.LookPath == nil {
		return "", false
	}
	p, err := e.LookPath(bin)
	return p, err == nil
}

// Has reports whether a path exists.
func (e Env) Has(path string) bool {
	if e.Exists != nil {
		return e.Exists(path)
	}
	_, err := os.Stat(path)
	return err == nil
}

// Store is the ownership state store under StateDir.
func (e Env) Store() ownership.Store { return ownership.Store{Dir: e.StateDir} }

// Clock returns the current time.
func (e Env) Clock() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// DetectState is how a runtime shows up on this machine (spec section 8).
type DetectState string

const (
	StateNone            DetectState = "none"             // no signal at all
	StateInstalled       DetectState = "installed"        // binary or app present
	StateConfigured      DetectState = "configured"       // installed and GabitoTL assets present
	StateDetectedHistory DetectState = "detected-history" // only leftover configuration, no binary
)

// Signal is one piece of evidence gathered by Detect.
type Signal struct {
	Name   string `json:"name"`
	Found  bool   `json:"found"`
	Detail string `json:"detail,omitempty"`
}

// Detection is the result of Adapter.Detect.
type Detection struct {
	Runtime string      `json:"runtime"`
	State   DetectState `json:"state"`
	Signals []Signal    `json:"signals"`
}

// Scope selects where configuration is written.
type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeProject Scope = "project"
)

// MemoryMCP describes the memory MCP server entry to configure. A zero value
// (empty Command) means "configure no memory server".
type MemoryMCP struct {
	Name    string   // preferred entry name, for example "engram"
	Command string   // for example "engram"
	Args    []string // for example ["mcp"]
}

// Names of the memory MCP entries GabitoTL manages.
const (
	EntryEngram = "engram"
	EntryBridge = "gabitotl-memory"
	// EntryCodeGraph is the CodeGraph code-index MCP entry (see Options.Tools).
	EntryCodeGraph = "codegraph"
)

// MemoryFor returns the memory MCP entries to configure and the entries that
// are obsolete. The choice of provider lives in each project's .gabitotl/memory.md,
// never in the machine configuration, so a known provider (engram or memory-mcp)
// always yields the same setup:
//
//   - main: `gabitotl memory mcp`, the bridge that reads the memory.md of the
//     project it runs in and talks to the team server or to Engram accordingly.
//   - extra: Engram's own `engram mcp` entry (its full native toolset) when the
//     engram binary is present, so Engram-backed projects keep it.
//   - retire: the `engram` entry, only when the binary is gone (the entry points
//     at nothing). An adapter removes it only if GabitoTL created it; an entry of
//     the user is never touched. A provider that is still wanted is never retired.
//
// An empty or unknown provider ("none") configures nothing and retires nothing.
func MemoryFor(provider string, engramPresent bool) (main MemoryMCP, extra []MemoryMCP, retire []string) {
	switch provider {
	case gabitotl.MemoryMemoryMCP, gabitotl.MemoryEngram:
		main = MemoryMCP{Name: EntryBridge, Command: "gabitotl", Args: []string{"memory", "mcp"}}
		if engramPresent {
			extra = []MemoryMCP{{Name: EntryEngram, Command: "engram", Args: []string{"mcp"}}}
		} else {
			retire = []string{EntryEngram}
		}
	}
	return main, extra, retire
}

// Asset is one whole file GabitoTL owns inside a runtime, for example a skill:
// Name is its directory under the runtime's skills directory and Content the
// SKILL.md text. The installer fills it from package skills (runtime must not
// import it), so adapters only decide WHERE each asset goes.
//
// Files are the other files of the skill directory (references/x.md), keyed by
// their path relative to it with forward slashes.
type Asset struct {
	Name    string
	Content string
	Files   map[string]string
}

// Options drive Plan.
type Options struct {
	Scope       Scope
	PersonaText string
	Memory      MemoryMCP
	// MemoryExtra are further memory MCP entries to configure next to Memory
	// (Engram's own server, see MemoryFor).
	MemoryExtra []MemoryMCP
	// Tools are non-memory stdio MCP servers GabitoTL registers the same way
	// (today only CodeGraph). They are planned through MemoryServers.
	Tools []MemoryMCP
	// RetireMemory lists memory MCP entry names GabitoTL may have created earlier
	// and that are now obsolete. An adapter plans their removal (REMOVE-AFTER-VERIFY)
	// only when the ownership state says GabitoTL created that exact entry: an entry
	// of the user is never touched. A configured entry is never retired.
	RetireMemory     []string
	Model            string // model chosen in runtime.md; adapters apply it only when set
	MigrateGentleman bool   // allow removing the managed Gentleman/Gentle-AI persona block
	Version          string // GabitoTL version recorded in state

	// Skills are the skill files to install under the runtime's skills directory.
	Skills []Asset
	// RetireSkills names optional skills that are not selected: an adapter plans
	// the removal of the files it wrote for them (and only those) after verify.
	RetireSkills []string
}

// MemoryServers lists every stdio MCP entry to configure: Memory, MemoryExtra, then Tools.
func (o Options) MemoryServers() []MemoryMCP {
	var out []MemoryMCP
	if o.Memory.Command != "" {
		out = append(out, o.Memory)
	}
	for _, m := range append(slices.Clone(o.MemoryExtra), o.Tools...) {
		if m.Command != "" {
			out = append(out, m)
		}
	}
	return out
}

// Kind is the verb of a planned Action (spec sections 32 and 42).
type Kind string

const (
	Create            Kind = "CREATE"
	Modify            Kind = "MODIFY"
	Preserve          Kind = "PRESERVE"
	RemoveAfterVerify Kind = "REMOVE-AFTER-VERIFY"
	NoAction          Kind = "NO-ACTION"
)

// Edit operations carried by an Action.
const (
	OpUpsertSection = "upsert-section"
	OpRemoveBlock   = "remove-foreign-block"
	OpSetKey        = "set-key"
	OpAddItem       = "add-item"
	OpRemoveKey     = "remove-key"  // remove a managed JSON key (REMOVE-AFTER-VERIFY)
	OpRemoveFile    = "remove-file" // remove a whole file GabitoTL wrote (REMOVE-AFTER-VERIFY)
	OpWriteFile     = "write-file"  // write a whole file GabitoTL owns (Edit.Body is the content)
	OpManual        = "manual"      // nothing is written; the user must act
)

// Edit is the executable payload of an Action. Plan builds it, Apply runs it.
type Edit struct {
	Op      string
	Name    string          // section name or block name
	Body    string          // section body
	Header  string          // text placed first when the file is created (for example front matter)
	Key     []string        // JSON key path
	Item    string          // JSON array item
	Value   json.RawMessage // JSON value for OpSetKey
	Foreign string          // OpRemoveBlock: marker family to match ("gentle-ai")
}

// Action is one planned change. Plans are pure: building them writes nothing.
type Action struct {
	Kind    Kind   `json:"kind"`
	Runtime string `json:"runtime"`
	Path    string `json:"path"`
	Target  string `json:"target"` // section, key or file label
	Detail  string `json:"detail,omitempty"`
	Edit    Edit   `json:"-"`
}

func (a Action) String() string {
	s := fmt.Sprintf("%-19s %s [%s]", a.Kind, a.Path, a.Target)
	if a.Detail != "" {
		s += " - " + a.Detail
	}
	return s
}

// Conflict is something Inspect found that Plan must address.
type Conflict struct {
	Kind   string `json:"kind"` // "persona"
	Path   string `json:"path"`
	Detail string `json:"detail"`
}

// Inspection is the read-only view of a runtime's existing configuration.
type Inspection struct {
	Runtime   string     `json:"runtime"`
	Files     []FileInfo `json:"files"`
	Conflicts []Conflict `json:"conflicts,omitempty"`
}

// FileInfo describes one configuration file an adapter cares about.
type FileInfo struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Managed bool   `json:"managed"` // GabitoTL already has assets in it
}

// Snapshot collects the files copied before any change (global backup, spec 40).
type Snapshot struct {
	Dir   string
	Files []string
}

// CopyPath is where the copy of the original file orig lives inside the
// snapshot. Backup and restore both use it, so they always agree.
func (s Snapshot) CopyPath(orig string) string {
	rel := strings.NewReplacer(":", "_", `\`, "/").Replace(orig)
	return filepath.Join(s.Dir, "files", strings.TrimLeft(rel, "/"))
}

// Check is one verification line.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Result summarizes Apply or RemoveGabitoConfig.
type Result struct {
	Applied []Action `json:"applied"`
	Manual  []Action `json:"manual,omitempty"`  // actions the user must do by hand
	Checks  []Check  `json:"checks,omitempty"`  // post-apply verification
	Removed []Action `json:"removed,omitempty"` // REMOVE-AFTER-VERIFY actions that ran
}

// Adapter configures one runtime (spec section 32). Core code never branches on
// runtime names: it iterates the Registry and calls these methods.
type Adapter interface {
	Name() string  // stable id used in runtime.md and state, for example "claude-code"
	Label() string // human name
	Detect(env Env) Detection
	Inspect(env Env, opts Options) (Inspection, error)
	// Backup copies the files plan would touch into snap.
	Backup(env Env, snap *Snapshot, plan []Action) error
	// Plan is pure: it reads existing files and returns the actions, writing nothing.
	Plan(env Env, opts Options) ([]Action, error)
	// Apply runs CREATE/MODIFY actions, verifies them and only then runs
	// REMOVE-AFTER-VERIFY actions.
	Apply(env Env, plan []Action) (Result, error)
	// Verify re-plans and reports every action that is still pending.
	Verify(env Env, opts Options) []Check
	// RemoveGabitoConfig removes exactly what the ownership state says we wrote.
	RemoveGabitoConfig(env Env) (Result, error)
	// ListModels is best effort and may return ErrUnsupported.
	ListModels(env Env) ([]string, error)
}

// SkillsDirer is implemented by adapters that install skills; it names the
// directory that holds <skill>/SKILL.md for a scope.
type SkillsDirer interface {
	SkillsDir(env Env, s Scope) string
}

// AttributionState is the read-only state of a runtime's own attribution
// setting (the line a coding tool adds to commits and pull requests).
type AttributionState struct {
	Supported bool   // the runtime documents a setting that controls attribution
	Hidden    bool   // the managed keys are in place, so the tool adds no attribution
	Custom    bool   // the user chose a different value, which GabitoTL keeps
	Detail    string // file or key concerned, for the report
}

// AttributionReporter is implemented by adapters that manage an attribution
// setting. Doctor reads it; adapters without it have no documented setting.
type AttributionReporter interface {
	Attribution(env Env, scope Scope) AttributionState
}

// Registry holds adapters in registration order.
type Registry struct{ adapters []Adapter }

// Register adds an adapter; duplicate names are a programming error.
func (r *Registry) Register(a Adapter) {
	if _, ok := r.Get(a.Name()); ok {
		panic("runtime: duplicate adapter " + a.Name())
	}
	r.adapters = append(r.adapters, a)
}

// All returns the adapters in registration order.
func (r *Registry) All() []Adapter { return append([]Adapter(nil), r.adapters...) }

// Get returns the adapter with the given name.
func (r *Registry) Get(name string) (Adapter, bool) {
	for _, a := range r.adapters {
		if a.Name() == name {
			return a, true
		}
	}
	return nil, false
}

// Names lists the registered names.
func (r *Registry) Names() []string {
	out := make([]string, len(r.adapters))
	for i, a := range r.adapters {
		out[i] = a.Name()
	}
	return out
}
