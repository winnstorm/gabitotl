package gabitotl

import (
	"fmt"
	"net/url"
	"regexp"
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

// MemoryConfig is the MERGED view of the team-memory settings. They live in two
// files (docs/DECISIONS.md D30): the shared, committed memory.md holds mode,
// provider, backend, project_key and the team-reachable url; the per-person,
// never committed local/users/<id>/memory.md holds role, instance, token,
// postgres_connection and an optional local url override. LoadMemory merges them
// (local wins) and SaveMemory splits them. Connection fields only hold
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
	// Instance is the NAME of the managed server instance this machine hosts (host
	// role only; empty for the default instance). A plain name, never a reference.
	Instance string `yaml:"instance,omitempty"`
}

// instanceNameRe mirrors the instance names of the managed PostgreSQL
// (internal/pgmanaged), which this package cannot import.
var instanceNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

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
	if c.Instance != "" && !instanceNameRe.MatchString(c.Instance) {
		return fmt.Errorf("instance: must be a plain name (lowercase letters, digits and dashes, at most 31 characters)")
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

// VCSPermissions are per-operation permissions: ask, allow or deny. Merge is
// optional in the file: an empty value reads as ask.
type VCSPermissions struct {
	Branches     string `yaml:"branches"`
	Commits      string `yaml:"commits"`
	Push         string `yaml:"push"`
	PullRequests string `yaml:"pull_requests"`
	Merge        string `yaml:"merge,omitempty"`
}

// Merge methods (merge_method) and chain strategies (chain).
const (
	MergeSquash = "squash"
	MergeMerge  = "merge"
	MergeRebase = "rebase"

	// ChainStacked slices work that exceeds pr_budget_lines into stacked PRs on its
	// own; ChainNone keeps one PR. Neither is ever a question.
	ChainStacked = "stacked"
	ChainNone    = "none"

	// DefaultPRBudgetLines is the size above which work is sliced into stacked PRs.
	DefaultPRBudgetLines = 400
)

// MergeMethods and ChainStrategies list the valid values.
var (
	MergeMethods    = []string{MergeSquash, MergeMerge, MergeRebase}
	ChainStrategies = []string{ChainStacked, ChainNone}
)

// VCSConfig is the MERGED view of the delivery settings (docs/DECISIONS.md D33).
// The team settings live in the committed vcs.md; auth and optional permission
// overrides live in the per-person, git-ignored local/users/<id>/vcs.md. Every
// field added after the first release is optional, so older files keep working.
type VCSConfig struct {
	Provider      string         `yaml:"provider"`
	BranchPolicy  string         `yaml:"branch_policy"`
	CommitPolicy  string         `yaml:"commit_policy"`
	PRPolicy      string         `yaml:"pr_policy"`
	RemoteActions string         `yaml:"remote_actions"`
	Auth          string         `yaml:"auth,omitempty"`
	Permissions   VCSPermissions `yaml:"permissions"`
	// MergeMethod is how a PR is merged (default squash); DeleteBranch removes the
	// branch after the merge (default true); DraftPRs opens PRs as drafts.
	MergeMethod  string `yaml:"merge_method,omitempty"`
	DeleteBranch bool   `yaml:"delete_branch"`
	DraftPRs     bool   `yaml:"draft_prs"`
	// BaseBranch is the branch PRs target; empty means the repository default.
	BaseBranch string `yaml:"base_branch,omitempty"`
	// PRBudgetLines and Chain define the slicing of large work. They are settings,
	// never questions: nothing asks about size or exceptions.
	PRBudgetLines int    `yaml:"pr_budget_lines,omitempty"`
	Chain         string `yaml:"chain,omitempty"`
	// Configured is set when the delivery questions were answered, so they are not
	// asked again.
	Configured bool `yaml:"configured,omitempty"`
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
		Permissions:   VCSPermissions{PermAsk, PermAsk, PermAsk, PermAsk, PermAsk},
		MergeMethod:   MergeSquash,
		DeleteBranch:  true,
		PRBudgetLines: DefaultPRBudgetLines,
		Chain:         ChainStacked,
	}
}

// IsConfigured reports whether the delivery questions are settled: the marker is
// set, or (a file from before the marker) any permission is not ask or remote
// actions are automatic. A file that is still the plain defaults is not.
func (c VCSConfig) IsConfigured() bool {
	if c.Configured || c.RemoteActions == "automatic" {
		return true
	}
	p := c.Permissions
	for _, v := range []string{p.Branches, p.Commits, p.Push, p.PullRequests, p.Merge} {
		if v != "" && v != PermAsk {
			return true
		}
	}
	return false
}

// EffectiveMergeMethod is merge_method with its default.
func (c VCSConfig) EffectiveMergeMethod() string {
	if c.MergeMethod == "" {
		return MergeSquash
	}
	return c.MergeMethod
}

// Budget is pr_budget_lines with its default.
func (c VCSConfig) Budget() int {
	if c.PRBudgetLines <= 0 {
		return DefaultPRBudgetLines
	}
	return c.PRBudgetLines
}

// ChainStrategy is chain with its default.
func (c VCSConfig) ChainStrategy() string {
	if c.Chain == "" {
		return ChainStacked
	}
	return c.Chain
}

// MergePermission is permissions.merge with its default (ask).
func (p VCSPermissions) MergePermission() string {
	if p.Merge == "" {
		return PermAsk
	}
	return p.Merge
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
	if c.Permissions.Merge != "" {
		if err := oneOf("permissions.merge", c.Permissions.Merge, PermAsk, PermAllow, PermDeny); err != nil {
			return err
		}
	}
	if c.MergeMethod != "" {
		if err := oneOf("merge_method", c.MergeMethod, MergeMethods...); err != nil {
			return err
		}
	}
	if c.Chain != "" {
		if err := oneOf("chain", c.Chain, ChainStrategies...); err != nil {
			return err
		}
	}
	if c.PRBudgetLines < 0 {
		return fmt.Errorf("pr_budget_lines: must not be negative")
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

func LoadRuntime(l Layout) (RuntimeConfig, error) {
	c := RuntimeConfig{AllowModelSwitch: true}
	return c, readConfig(l.Runtime(), &c)
}

func LoadWorkflow(l Layout) (WorkflowConfig, error) {
	c := DefaultWorkflow()
	return c, readConfig(l.Workflow(), &c)
}

// Save helpers replace the front matter and keep the existing Markdown body.

func SaveProject(l Layout, c ProjectConfig) error {
	return saveConfig(l.Project(), &c, bodyProject)
}
func SaveRuntime(l Layout, c RuntimeConfig) error {
	return saveConfig(l.Runtime(), &c, bodyRuntime)
}
func SaveWorkflow(l Layout, c WorkflowConfig) error {
	return saveConfig(l.Workflow(), &c, bodyWorkflow)
}
