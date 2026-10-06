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
}

// OSEnv builds the production Env for a project root.
func OSEnv(goos, projectRoot string) (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, err
	}
	return Env{
		Home: home, ProjectRoot: projectRoot, StateDir: filepath.Join(home, ".gabitotl"), GOOS: goos,
		Getenv: os.Getenv, LookPath: exec.LookPath, Now: time.Now,
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
)

// MemoryFor returns the memory MCP entry to configure for a provider, and the
// entries of the other provider that a switch must retire. engramPresent says
// whether Engram is installed (a missing Engram is never wired).
//
//   - engram: `engram mcp`; retires the team bridge entry.
//   - memory-mcp: `gabitotl memory mcp`, which forwards to the team server over
//     HTTP; retires the engram entry. Runtimes only ever need this stdio command.
func MemoryFor(provider string, engramPresent bool) (MemoryMCP, []string) {
	switch provider {
	case gabitotl.MemoryMemoryMCP:
		return MemoryMCP{Name: EntryBridge, Command: "gabitotl", Args: []string{"memory", "mcp"}}, []string{EntryEngram}
	case gabitotl.MemoryEngram:
		if engramPresent {
			return MemoryMCP{Name: EntryEngram, Command: "engram", Args: []string{"mcp"}}, []string{EntryBridge}
		}
		return MemoryMCP{}, []string{EntryBridge}
	}
	return MemoryMCP{}, nil
}

// Options drive Plan.
type Options struct {
	Scope       Scope
	PersonaText string
	Memory      MemoryMCP
	// RetireMemory lists memory MCP entry names GabitoTL may have created earlier
	// and that the requested Memory replaces (switching between Engram and the team
	// bridge). An adapter plans their removal (REMOVE-AFTER-VERIFY) only when the
	// ownership state says GabitoTL created that exact entry: an entry of the user
	// is never touched.
	RetireMemory     []string
	Model            string // model chosen in runtime.md; adapters apply it only when set
	MigrateGentleman bool   // allow removing the managed Gentleman/Gentle-AI persona block
	Version          string // GabitoTL version recorded in state
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
	OpRemoveKey     = "remove-key" // remove a managed JSON key (REMOVE-AFTER-VERIFY)
	OpManual        = "manual"     // nothing is written; the user must act
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
