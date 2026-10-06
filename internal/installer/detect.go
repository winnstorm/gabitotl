package installer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/engram"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

// Phase is one step of the installer pipeline, in execution order (spec 5).
type Phase string

// Phases in the order they run. Retire only ever follows both verify phases.
const (
	PhasePlatform      Phase = "platform"
	PhaseProject       Phase = "project"
	PhaseRuntimes      Phase = "runtimes"
	PhaseMemory        Phase = "memory"
	PhaseOtherHarness  Phase = "other-harness"
	PhaseInventory     Phase = "inventory"
	PhasePlan          Phase = "plan"
	PhaseSnapshot      Phase = "snapshot"
	PhaseMigrate       Phase = "migrate-knowledge"
	PhaseConfigure     Phase = "configure-runtimes"
	PhaseVerifyMemory  Phase = "verify-memory"
	PhaseVerifyProject Phase = "verify-project"
	PhaseRetire        Phase = "retire"
	PhaseReport        Phase = "report"
)

// PhaseOrder lists every phase in order.
var PhaseOrder = []Phase{
	PhasePlatform, PhaseProject, PhaseRuntimes, PhaseMemory, PhaseOtherHarness, PhaseInventory,
	PhasePlan, PhaseSnapshot, PhaseMigrate, PhaseConfigure, PhaseVerifyMemory, PhaseVerifyProject,
	PhaseRetire, PhaseReport,
}

// Platform is the result of DetectPlatform.
type Platform struct {
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	Shell          string `json:"shell"`
	Home           string `json:"home"`
	Git            bool   `json:"git"`
	GitPath        string `json:"git_path,omitempty"`
	PackageManager string `json:"package_manager,omitempty"`
}

var packageManagers = map[string][]string{
	"linux":   {"apt", "dnf", "pacman", "brew"},
	"darwin":  {"brew", "port"},
	"windows": {"winget", "choco", "scoop"},
}

// DetectPlatform reads OS, architecture, shell, HOME, Git and the package
// manager. The installer never depends on a specific package manager.
func DetectPlatform(env Env) Platform {
	p := Platform{OS: env.GOOS, Arch: env.GOARCH, Home: env.Home}
	switch env.GOOS {
	case "windows":
		p.Shell = filepath.Base(env.Var("ComSpec"))
		if p.Shell == "." {
			p.Shell = ""
		}
	default:
		if s := env.Var("SHELL"); s != "" {
			p.Shell = filepath.Base(s)
		}
	}
	p.GitPath, p.Git = env.Look("git")
	for _, pm := range packageManagers[env.GOOS] {
		if _, ok := env.Look(pm); ok {
			p.PackageManager = pm
			break
		}
	}
	return p
}

// Project is the result of DetectProject.
type Project struct {
	Root     string `json:"root"`
	Name     string `json:"name"`
	Key      string `json:"key"` // memory project key (memory.md or slug of the name)
	Exists   bool   `json:"exists"`
	Kind     string `json:"kind"` // new | existing
	GitRepo  bool   `json:"git_repo"`
	Remote   string `json:"-"` // never reported: remotes can embed credentials
	Provider string `json:"vcs_provider"`
}

// DetectProject inspects the project root (no root means no project).
func DetectProject(ctx context.Context, env Env) Project {
	p := Project{Root: env.ProjectRoot}
	if p.Root == "" {
		return p
	}
	p.Name = filepath.Base(p.Root)
	p.Key = gabitotl.Slug(p.Name)
	p.Exists = gabitotl.Exists(p.Root)
	if k, err := gabitotl.DetectKind(p.Root); err == nil {
		p.Kind = k
	}
	if out, _, err := env.run(ctx, p.Root, "git", "rev-parse", "--is-inside-work-tree"); err == nil && strings.TrimSpace(string(out)) == "true" {
		p.GitRepo = true
		if out, _, err := env.run(ctx, p.Root, "git", "remote", "get-url", "origin"); err == nil {
			p.Remote = strings.TrimSpace(string(out))
			p.Provider = vcs.InferProvider(p.Remote)
		}
	}
	if p.Exists {
		l := gabitotl.NewLayout(p.Root)
		if c, err := gabitotl.LoadProject(l); err == nil && c.Name != "" {
			p.Name = c.Name
		}
		if m, err := gabitotl.LoadMemory(l); err == nil && m.ProjectKey != "" {
			p.Key = m.ProjectKey
		}
	}
	return p
}

// RuntimeStatus is one runtime as found on this machine.
type RuntimeStatus struct {
	Name      string         `json:"name"`
	Label     string         `json:"label"`
	State     rt.DetectState `json:"state"`
	Signals   []rt.Signal    `json:"signals"`
	Conflicts []rt.Conflict  `json:"conflicts,omitempty"` // foreign persona blocks found
}

// Usable reports whether the runtime has a binary or app (installed/configured).
func (r RuntimeStatus) Usable() bool {
	return r.State == rt.StateInstalled || r.State == rt.StateConfigured
}

// DetectRuntimes asks every registered adapter; core never names a runtime.
func DetectRuntimes(env Env) []RuntimeStatus {
	var out []RuntimeStatus
	if env.Runtimes == nil {
		return out
	}
	renv := env.RT()
	for _, a := range env.Runtimes.All() {
		d := a.Detect(renv)
		s := RuntimeStatus{Name: a.Name(), Label: a.Label(), State: d.State, Signals: d.Signals}
		if in, err := a.Inspect(renv, rt.Options{Scope: rt.ScopeGlobal}); err == nil {
			s.Conflicts = in.Conflicts
		}
		out = append(out, s)
	}
	return out
}

// Warning codes raised by DetectMemory. They are the "Ojo" of spec 55: an
// Engram installation is not necessarily a trivial local one.
const (
	WarnDataDirOverride = "data-dir-override"
	WarnCloudSync       = "cloud-sync"
	WarnProjectConfig   = "project-config"
	WarnProjectIdentity = "project-identity"
)

// Warning is one non-trivial-topology finding.
type Warning struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// MemoryInfo is the result of DetectMemory (Engram detected on its own,
// independent of any harness).
type MemoryInfo struct {
	HasBinary     bool                 `json:"has_binary"`
	Binary        string               `json:"binary,omitempty"`
	DataDir       string               `json:"data_dir"`
	DataDirSource string               `json:"data_dir_source"` // ENGRAM_DATA_DIR | default
	HasDir        bool                 `json:"has_dir"`
	HasDB         bool                 `json:"has_db"`
	ProjectDir    bool                 `json:"project_dir"` // <project>/.engram exists
	ProjectConfig string               `json:"project_config,omitempty"`
	DoctorRan     bool                 `json:"doctor_ran"`
	DoctorOK      bool                 `json:"doctor_ok"`
	DoctorChecks  []memory.Check       `json:"doctor_checks,omitempty"`
	Projects      []engram.ProjectInfo `json:"projects,omitempty"`
	ProjectsError string               `json:"projects_error,omitempty"`
	ProjectKey    string               `json:"project_key,omitempty"`
	ProjectMatch  string               `json:"project_match,omitempty"` // Engram project that matches the key
	Observations  int                  `json:"observations"`
	Warnings      []Warning            `json:"warnings,omitempty"`
}

// Present reports whether Engram exists in any form worth adopting.
func (m MemoryInfo) Present() bool { return m.HasBinary || m.HasDir || m.ProjectDir }

// DetectMemory looks at Engram: binary, data dir, .engram/, `engram doctor`
// (read-only) and `engram projects list` (read-only).
func DetectMemory(ctx context.Context, env Env, pr Project) MemoryInfo {
	m := MemoryInfo{DataDirSource: "default", ProjectKey: pr.Key}
	m.DataDir = filepath.Join(env.Home, ".engram")
	if d := env.Var("ENGRAM_DATA_DIR"); d != "" {
		m.DataDir, m.DataDirSource = d, "ENGRAM_DATA_DIR"
		m.Warnings = append(m.Warnings, Warning{Code: WarnDataDirOverride, Detail: d})
	}
	m.HasDir = isDir(m.DataDir)
	m.HasDB = isFile(filepath.Join(m.DataDir, "engram.db"))
	var cfgProject string
	if pr.Root != "" {
		pdir := filepath.Join(pr.Root, ".engram")
		m.ProjectDir = isDir(pdir)
		for _, f := range []string{"config.json", "manifest.json"} {
			b, err := os.ReadFile(filepath.Join(pdir, f))
			if err != nil {
				continue
			}
			var cfg map[string]any
			if json.Unmarshal(b, &cfg) != nil {
				continue
			}
			if s, ok := cfg["project"].(string); ok && cfgProject == "" {
				cfgProject = s
			}
			for k := range cfg {
				if lk := strings.ToLower(k); strings.Contains(lk, "cloud") || strings.Contains(lk, "sync") {
					m.Warnings = append(m.Warnings, Warning{Code: WarnCloudSync, Detail: ".engram/" + f + ": " + k})
				}
			}
		}
		if m.ProjectDir {
			m.ProjectConfig = cfgProject
			m.Warnings = append(m.Warnings, Warning{Code: WarnProjectConfig, Detail: cfgProject})
		}
	}
	for _, kv := range env.vars() {
		name, _, _ := strings.Cut(kv, "=")
		if u := strings.ToUpper(name); strings.HasPrefix(u, "ENGRAM_") && (strings.Contains(u, "CLOUD") || strings.Contains(u, "SYNC")) {
			m.Warnings = append(m.Warnings, Warning{Code: WarnCloudSync, Detail: name}) // the name only, never the value
		}
	}

	p, bin, ok := env.Engram(pr.Key)
	m.HasBinary, m.Binary = ok, bin
	if ok {
		rep := p.Doctor(ctx)
		m.DoctorRan, m.DoctorOK, m.DoctorChecks = true, rep.OK(), rep.Checks
		ps, err := p.Projects(ctx)
		if err != nil {
			m.ProjectsError = err.Error()
		}
		m.Projects = ps
	}
	for _, p := range m.Projects {
		if p.Name == pr.Key {
			m.ProjectMatch, m.Observations = p.Name, p.Observations
		}
	}
	if m.ProjectMatch == "" {
		for _, p := range m.Projects {
			if strings.EqualFold(p.Name, pr.Key) || (cfgProject != "" && p.Name == cfgProject) {
				m.ProjectMatch, m.Observations = p.Name, p.Observations
				m.Warnings = append(m.Warnings, Warning{Code: WarnProjectIdentity, Detail: p.Name + " != " + pr.Key})
				break
			}
		}
	}
	if cfgProject != "" && pr.Key != "" && cfgProject != pr.Key && m.ProjectMatch != cfgProject {
		m.Warnings = append(m.Warnings, Warning{Code: WarnProjectIdentity, Detail: ".engram project " + cfgProject + " != " + pr.Key})
	}
	return m
}

// AgentRef is one agent Gentle-AI manages.
type AgentRef struct {
	ID      string `json:"id"`
	Runtime string `json:"runtime,omitempty"` // registered GabitoTL runtime with the same id, "" when unsupported in V1
}

// GentleAI is the result of detecting Gentle-AI (independent from Engram).
type GentleAI struct {
	Found      bool       `json:"found"`
	Binary     string     `json:"binary,omitempty"`
	HasState   bool       `json:"has_state"`
	HasHome    bool       `json:"has_home"`
	HasBackups bool       `json:"has_backups"` // ~/.gentle-ai/backups exists (never touched)
	Version    string     `json:"version,omitempty"`
	Agents     []AgentRef `json:"agents,omitempty"`
	Components []string   `json:"components,omitempty"`
	StateError string     `json:"state_error,omitempty"`
}

// OtherHarness is the result of DetectOtherHarness.
type OtherHarness struct {
	GentleAI GentleAI `json:"gentle_ai"`
}

// GentleHome is ~/.gentle-ai.
func GentleHome(env Env) string { return filepath.Join(env.Home, ".gentle-ai") }

// GentleStatePath is ~/.gentle-ai/state.json.
func GentleStatePath(env Env) string { return filepath.Join(GentleHome(env), "state.json") }

// DetectOtherHarness finds Gentle-AI through its binary, ~/.gentle-ai and the
// agents/components recorded in state.json (read-only).
func DetectOtherHarness(env Env) OtherHarness {
	g := GentleAI{}
	g.Binary, _ = env.Look("gentle-ai")
	g.HasHome = isDir(GentleHome(env))
	g.HasBackups = isDir(filepath.Join(GentleHome(env), "backups"))
	g.HasState = isFile(GentleStatePath(env))
	g.Found = g.Binary != "" || g.HasHome || g.HasState
	if g.HasState {
		b, err := os.ReadFile(GentleStatePath(env))
		var st map[string]any
		switch {
		case err != nil:
			g.StateError = err.Error()
		case json.Unmarshal(b, &st) != nil:
			g.StateError = "state.json is not valid JSON"
		default:
			g.Version, _ = st["version"].(string)
			for _, id := range stateNames(st["installed_agents"]) {
				ref := AgentRef{ID: id}
				if env.Runtimes != nil {
					if _, ok := env.Runtimes.Get(id); ok {
						ref.Runtime = id
					}
				}
				g.Agents = append(g.Agents, ref)
			}
			g.Components = stateNames(st["components"])
		}
	}
	return OtherHarness{GentleAI: g}
}

// stateNames extracts names from a JSON list of strings or objects (name/id/
// agent field) or from an object's keys: the real state.json schema is not
// pinned down, so parsing is tolerant.
func stateNames(v any) []string {
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

func isDir(p string) bool  { st, err := os.Stat(p); return err == nil && st.IsDir() }
func isFile(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }
