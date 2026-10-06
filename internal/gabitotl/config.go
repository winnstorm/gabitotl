package gabitotl

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Enumerations (front matter values are exactly these strings).
const (
	KindNew      = "new"
	KindExisting = "existing"

	ModeSolo = "solo"
	ModeTeam = "team"

	MemoryEngram    = "engram"
	MemoryMemoryMCP = "memory-mcp"

	PermAsk   = "ask"
	PermAllow = "allow"
	PermDeny  = "deny"
)

// VCSProviders lists the supported remote providers.
var VCSProviders = []string{"github", "bitbucket", "gitlab", "azure-devops", "git", "local"}

func oneOf(field, v string, allowed ...string) error {
	if !slices.Contains(allowed, v) {
		return fmt.Errorf("%s: %q is not one of %v", field, v, allowed)
	}
	return nil
}

// ProjectConfig is the front matter of project.md.
type ProjectConfig struct {
	Name        string `yaml:"name"`
	ProjectKind string `yaml:"project_kind"`
	Status      string `yaml:"status"`
}

// DefaultProject returns defaults for a project called name.
func DefaultProject(name, kind string) ProjectConfig {
	return ProjectConfig{Name: name, ProjectKind: kind, Status: "active"}
}

func (c *ProjectConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("name: required")
	}
	if c.Status == "" {
		return fmt.Errorf("status: required")
	}
	return oneOf("project_kind", c.ProjectKind, KindNew, KindExisting)
}

// MemoryConfig is the front matter of memory.md. Connection fields only hold
// references (env:NAME, keychain:NAME, native), never secrets.
type MemoryConfig struct {
	Mode               string `yaml:"mode"`
	Provider           string `yaml:"provider"`
	Backend            string `yaml:"backend,omitempty"`
	ProjectKey         string `yaml:"project_key"`
	PostgresConnection string `yaml:"postgres_connection,omitempty"`
	// Team memory (provider memory-mcp): the server endpoint and the NAME of the
	// environment variable that holds this developer's token. Never the token.
	URL   string `yaml:"url,omitempty"`
	Token string `yaml:"token,omitempty"`
	// Role is "host" on the machine that runs the server and "client" elsewhere.
	Role string `yaml:"role,omitempty"`
}

// Memory roles for provider memory-mcp.
const (
	RoleHost   = "host"
	RoleClient = "client"
)

func (c *MemoryConfig) Validate() error {
	if err := oneOf("mode", c.Mode, ModeSolo, ModeTeam); err != nil {
		return err
	}
	if err := oneOf("provider", c.Provider, MemoryEngram, MemoryMemoryMCP); err != nil {
		return err
	}
	if c.ProjectKey == "" {
		return fmt.Errorf("project_key: required")
	}
	if c.PostgresConnection != "" && !IsSafeRef(c.PostgresConnection) {
		return fmt.Errorf("postgres_connection: must be a reference (env:NAME or keychain:NAME)")
	}
	if c.Role != "" {
		if err := oneOf("role", c.Role, RoleHost, RoleClient); err != nil {
			return err
		}
	}
	if c.URL != "" {
		u, err := url.Parse(c.URL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			return fmt.Errorf("url: must look like https://host:port/mcp (no credentials)")
		}
	}
	if c.Token != "" && !(strings.HasPrefix(c.Token, "env:") && len(c.Token) > 4) {
		return fmt.Errorf("token: must be an env:NAME reference to the variable that holds your token, never the token itself")
	}
	return nil
}

// RuntimeConfig is the front matter of runtime.md. The runtime name is an
// opaque string here: core code never branches on it.
type RuntimeConfig struct {
	Runtime          string `yaml:"runtime"`
	Model            string `yaml:"model,omitempty"`
	AllowModelSwitch bool   `yaml:"allow_model_switch"`
}

func (c *RuntimeConfig) Validate() error {
	if c.Runtime == "" {
		return fmt.Errorf("runtime: required")
	}
	return nil
}

// VCSPermissions are per-operation permissions: ask, allow or deny.
type VCSPermissions struct {
	Branches     string `yaml:"branches"`
	Commits      string `yaml:"commits"`
	Push         string `yaml:"push"`
	PullRequests string `yaml:"pull_requests"`
}

// VCSConfig is the front matter of vcs.md.
type VCSConfig struct {
	Provider      string         `yaml:"provider"`
	BranchPolicy  string         `yaml:"branch_policy"`
	CommitPolicy  string         `yaml:"commit_policy"`
	PRPolicy      string         `yaml:"pr_policy"`
	RemoteActions string         `yaml:"remote_actions"`
	Auth          string         `yaml:"auth,omitempty"`
	Permissions   VCSPermissions `yaml:"permissions"`
}

// DefaultVCS returns conservative defaults: remote actions and every
// operation require confirmation.
func DefaultVCS(provider string) VCSConfig {
	return VCSConfig{
		Provider:      provider,
		BranchPolicy:  "task",
		CommitPolicy:  "milestone",
		PRPolicy:      "task-complete",
		RemoteActions: "ask",
		Permissions:   VCSPermissions{PermAsk, PermAsk, PermAsk, PermAsk},
	}
}

func (c *VCSConfig) Validate() error {
	if err := oneOf("provider", c.Provider, VCSProviders...); err != nil {
		return err
	}
	if c.Auth != "" && !IsSafeRef(c.Auth) {
		return fmt.Errorf("auth: must be a reference (native, env:NAME or keychain:NAME)")
	}
	for name, v := range map[string]string{
		"permissions.branches":      c.Permissions.Branches,
		"permissions.commits":       c.Permissions.Commits,
		"permissions.push":          c.Permissions.Push,
		"permissions.pull_requests": c.Permissions.PullRequests,
	} {
		if err := oneOf(name, v, PermAsk, PermAllow, PermDeny); err != nil {
			return err
		}
	}
	return nil
}

// BackupPolicy is the snapshot retention policy of .bak/.
type BackupPolicy struct {
	MaxSnapshots int `yaml:"max_snapshots"`
	MaxAgeDays   int `yaml:"max_age_days"`
}

// WorkflowConfig is the front matter of workflow.md.
type WorkflowConfig struct {
	Output            string       `yaml:"output"`
	Language          string       `yaml:"language"`
	Persona           string       `yaml:"persona"`
	BackupBeforeWrite bool         `yaml:"backup_before_write"`
	MemoryBeforeWork  bool         `yaml:"memory_before_work"`
	MemoryAfterWork   bool         `yaml:"memory_after_work"`
	Backup            BackupPolicy `yaml:"backup"`
	// Context budget (spec section 20). Missing keys keep these defaults, so
	// workflow.md files written before they existed keep working.
	ContextLimitTokens int     `yaml:"context_limit_tokens"`
	ContextThreshold   float64 `yaml:"context_threshold"`
	// Authorship guard (docs/GUARD.md). Missing keys keep these defaults.
	BrandingGuard      string   `yaml:"branding_guard"`
	BrandingExtraTerms []string `yaml:"branding_extra_terms"`
	BrandingAllowPaths []string `yaml:"branding_allow_paths"`
	// GuardHooks remembers the answer to "install the Git hooks?": ask (not
	// answered yet), installed or declined.
	GuardHooks string `yaml:"guard_hooks"`
}

// Branding guard modes (branding_guard).
const (
	// GuardStrip removes attribution lines from messages (commits, PR text,
	// memory entries) and reports what it removed; it blocks anything it cannot
	// remove safely, including attribution in files.
	GuardStrip = "strip"
	// GuardBlock refuses everywhere and never edits.
	GuardBlock = "block"
	// GuardWarn reports findings and lets the operation go on.
	GuardWarn = "warn"
	// GuardOff disables enforcement (scan and fix still work on demand).
	GuardOff = "off"
)

// Guard hook answers (guard_hooks).
const (
	HooksAsk       = "ask"
	HooksInstalled = "installed"
	HooksDeclined  = "declined"
)

// GuardModes lists the valid branding_guard values.
var GuardModes = []string{GuardStrip, GuardBlock, GuardWarn, GuardOff}

// Context budget defaults.
const (
	DefaultContextLimitTokens = 200_000
	DefaultContextThreshold   = 0.75
)

// DefaultWorkflow returns the V1 workflow defaults.
func DefaultWorkflow() WorkflowConfig {
	return WorkflowConfig{
		Output: "compact", Language: "es-AR", Persona: "gabito",
		BackupBeforeWrite: true, MemoryBeforeWork: true, MemoryAfterWork: true,
		Backup:             BackupPolicy{MaxSnapshots: 20, MaxAgeDays: 7},
		ContextLimitTokens: DefaultContextLimitTokens, ContextThreshold: DefaultContextThreshold,
		BrandingGuard: GuardStrip, GuardHooks: HooksAsk,
		BrandingExtraTerms: []string{}, BrandingAllowPaths: []string{},
	}
}

func (c *WorkflowConfig) Validate() error {
	if c.Output == "" || c.Language == "" || c.Persona == "" {
		return fmt.Errorf("output, language and persona are required")
	}
	if c.Backup.MaxSnapshots < 1 || c.Backup.MaxAgeDays < 1 {
		return fmt.Errorf("backup: max_snapshots and max_age_days must be >= 1")
	}
	if c.ContextLimitTokens <= 0 {
		return fmt.Errorf("context_limit_tokens: must be positive")
	}
	if c.ContextThreshold <= 0 || c.ContextThreshold > 1 {
		return fmt.Errorf("context_threshold: must be in (0, 1]")
	}
	if err := oneOf("branding_guard", c.BrandingGuard, GuardModes...); err != nil {
		return err
	}
	return oneOf("guard_hooks", c.GuardHooks, HooksAsk, HooksInstalled, HooksDeclined)
}

// Load helpers: each decodes into defaults, then validates.

func LoadProject(l Layout) (ProjectConfig, error) {
	c := DefaultProject("", KindNew)
	return c, readConfig(l.Project(), &c)
}

func LoadMemory(l Layout) (MemoryConfig, error) {
	var c MemoryConfig
	return c, readConfig(l.Memory(), &c)
}

func LoadRuntime(l Layout) (RuntimeConfig, error) {
	c := RuntimeConfig{AllowModelSwitch: true}
	return c, readConfig(l.Runtime(), &c)
}

func LoadVCS(l Layout) (VCSConfig, error) {
	c := DefaultVCS("git")
	return c, readConfig(l.VCS(), &c)
}

func LoadWorkflow(l Layout) (WorkflowConfig, error) {
	c := DefaultWorkflow()
	return c, readConfig(l.Workflow(), &c)
}

// Save helpers replace the front matter and keep the existing Markdown body.

func SaveProject(l Layout, c ProjectConfig) error {
	return saveConfig(l.Project(), &c, bodyProject)
}
func SaveMemory(l Layout, c MemoryConfig) error { return saveConfig(l.Memory(), &c, bodyMemory) }
func SaveRuntime(l Layout, c RuntimeConfig) error {
	return saveConfig(l.Runtime(), &c, bodyRuntime)
}
func SaveVCS(l Layout, c VCSConfig) error { return saveConfig(l.VCS(), &c, bodyVCS) }
func SaveWorkflow(l Layout, c WorkflowConfig) error {
	return saveConfig(l.Workflow(), &c, bodyWorkflow)
}
