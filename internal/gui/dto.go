package gui

import "errors"

// Errors returned by the facade. They are typed so the shell can show a proper
// message instead of a raw string.
var (
	// ErrNotAvailable marks a mockup option that has no backing code yet.
	ErrNotAvailable = errors.New("not available yet")
	// ErrUnsupportedOS is returned by operations that only exist on Windows.
	ErrUnsupportedOS = errors.New("not supported on this operating system")
	// ErrPlanRequired means a mutating call was made without the plan id the
	// matching Plan call returned (the user must see the plan first).
	ErrPlanRequired = errors.New("review the plan first: pass the plan id it returned")
	// ErrPlanChanged means the machine changed between the plan and the apply.
	ErrPlanChanged = errors.New("the plan changed since it was shown: review it again")
	// ErrDecisionPending means a choice that must be settled first is still open.
	ErrDecisionPending = errors.New("a decision is still pending")
	// ErrCLINotInstalled means the guard hooks cannot be installed because no
	// installed gabitotl executable is known; the hooks must never call the
	// setup executable or anything in Downloads.
	// ErrBusy means another long operation is still running.
	ErrBusy            = errors.New("operation in progress")
	ErrCLINotInstalled = errors.New("the gabitotl CLI is not installed yet")
)

// Event is one progress notification of a long operation. The shell forwards it
// to the page as it arrives.
type Event struct {
	Step    string `json:"step"`
	Status  string `json:"status"` // running | done | warn | error | skipped
	Message string `json:"message"`
	Percent int    `json:"percent"` // 0..100, monotonic within one operation
}

// Progress receives events; nil is allowed everywhere.
type Progress func(Event)

func (p Progress) emit(step, status, msg string, pct int) {
	if p != nil {
		p(Event{Step: step, Status: status, Message: msg, Percent: pct})
	}
}

// Step status values.
const (
	StatusOK          = "ok"
	StatusSkipped     = "skipped"
	StatusUnsupported = "unsupported"
	StatusError       = "error"
)

// StepResult is what one optional extra step of an apply did.
type StepResult struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // ok | skipped | unsupported | error
	Message string `json:"message,omitempty"`
}

// PlanGroup is one verb of a plan (CREATE, MODIFY, ...).
type PlanGroup struct {
	Kind  string   `json:"kind"`
	Label string   `json:"label"`
	Lines []string `json:"lines"`
}

// Note is a coded heads-up with its localized text.
type Note struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// PlanStep is an extra step the apply runs next to the installer plan (persona
// mode, code index, Windows integration).
type PlanStep struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Plan is what the review screen shows before anything changes. Pass ID back to
// the matching apply call.
type Plan struct {
	ID         string      `json:"id"`
	Groups     []PlanGroup `json:"groups"`
	Notes      []Note      `json:"notes"`
	Steps      []PlanStep  `json:"steps"`
	Mutates    bool        `json:"mutates"`
	Supported  bool        `json:"supported"`             // false: nothing here can run on this OS
	Snapshot   string      `json:"snapshot,omitempty"`    // id the backup will get (informational)
	Unsupports []string    `json:"unsupported,omitempty"` // steps skipped on this OS
}

// DetectResult is the inventory of the second wizard screen.
type DetectResult struct {
	Root         string          `json:"root"`
	ProjectName  string          `json:"projectName"`
	ProjectReady bool            `json:"projectReady"` // .gabitotl already exists
	OS           string          `json:"os"`
	Arch         string          `json:"arch"`
	Git          GitInfo         `json:"git"`
	Runtimes     []RuntimeInfo   `json:"runtimes"`
	Engram       EngramInfo      `json:"engram"`
	Container    string          `json:"containerEngine"` // docker | podman | ""
	PackageMgr   string          `json:"packageManager"`
	Gentle       GentleInfo      `json:"gentleAI"`
	UserName     string          `json:"userName"` // git config user.name, the profile default
	Language     string          `json:"language"` // saved language (es | en)
	PersonaMode  string          `json:"personaMode"`
	CodeIndex    []IndexBackend  `json:"codeIndex"`
	Desktop      DesktopStatus   `json:"desktop"`
	Sections     []InventoryPart `json:"sections"` // the same rows as the CLI summary
}

// GitInfo is the Git part of the inventory.
type GitInfo struct {
	Found    bool   `json:"found"`
	Path     string `json:"path,omitempty"`
	Repo     bool   `json:"repo"`
	Provider string `json:"provider,omitempty"` // inferred from origin: github | gitlab | bitbucket
}

// RuntimeInfo is one coding agent as found on the machine.
type RuntimeInfo struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	State    string `json:"state"`
	Usable   bool   `json:"usable"`
	Conflict bool   `json:"personaConflict"` // a foreign persona block shares its instruction file
}

// EngramInfo is the Engram part of the inventory.
type EngramInfo struct {
	Present      bool `json:"present"`
	HasBinary    bool `json:"hasBinary"`
	Observations int  `json:"observations"`
	CanImport    bool `json:"canImport"` // a tested Engram migrator exists
}

// GentleInfo is the Gentle AI part of the inventory. Action is the default
// choice: migrate | keep | review ("" when Gentle AI was not found).
type GentleInfo struct {
	Found             bool     `json:"found"`
	Version           string   `json:"version,omitempty"`
	Agents            []string `json:"agents,omitempty"`
	Action            string   `json:"action"`
	PersonaConflict   bool     `json:"personaConflict"`
	GentleKiroPresent bool     `json:"gentleKiroPresent"`
}

// InventoryPart is a titled group of summary rows.
type InventoryPart struct {
	Title string   `json:"title"`
	Rows  []string `json:"rows"`
}

// IndexBackend is one optional code index tool.
type IndexBackend struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Indexed   bool   `json:"indexed"`
	Hint      string `json:"installHint,omitempty"`
}

// DesktopStatus reports the Windows integration (Supported is false elsewhere).
type DesktopStatus struct {
	Supported   bool   `json:"supported"`
	InstallDir  string `json:"installDir,omitempty"`
	Installed   bool   `json:"installed"`
	Version     string `json:"version,omitempty"`
	OnPath      bool   `json:"onPath"`
	StartMenu   bool   `json:"startMenu"`
	DesktopLink bool   `json:"desktopShortcut"`
}

// MemoryAnswers is the memory screen: solo (Engram) or team (memory-mcp).
type MemoryAnswers struct {
	Mode         string   `json:"mode"` // solo | team
	Role         string   `json:"role"` // team: host | client
	Engine       string   `json:"engine"`
	Port         int      `json:"port"`
	Instance     string   `json:"instance"`
	URL          string   `json:"url"`
	TokenEnv     string   `json:"tokenEnv"`
	StoreToken   bool     `json:"storeToken"`
	ImportEngram bool     `json:"importEngram"`
	Invites      []string `json:"invites"`
}

// DeliveryAnswers is the delivery screen.
type DeliveryAnswers struct {
	Provider     string `json:"provider"`   // github | gitlab | bitbucket | none | "" (keep)
	Automation   string `json:"automation"` // all | until-pr | ask | ""
	MergeMethod  string `json:"mergeMethod"`
	DeleteBranch *bool  `json:"deleteBranch"`
	Draft        *bool  `json:"draft"`
	TokenEnv     string `json:"tokenEnv"` // Bitbucket: NAME of the variable that holds the token
}

// WindowsAnswers is the Windows screen.
type WindowsAnswers struct {
	InstallDir      string `json:"installDir"`
	StartMenu       bool   `json:"startMenu"`
	DesktopShortcut bool   `json:"desktopShortcut"`
	AddToPath       bool   `json:"addToPath"`
	GuardHooks      bool   `json:"guardHooks"`
	CodeIndex       string `json:"codeIndex"` // codegraph | code-review-graph | none
}

// InstallAnswers is everything the wizard collected. The GUI asks up front: the
// facade never prompts.
type InstallAnswers struct {
	Root             string          `json:"root"`
	Lang             string          `json:"lang"`         // es | en
	GentleAction     string          `json:"gentleAction"` // migrate | keep | review
	Name             string          `json:"name"`
	User             string          `json:"user"`
	PersonaMode      string          `json:"personaMode"` // lite | normal | full
	Runtimes         []string        `json:"runtimes"`
	Scope            string          `json:"scope"` // global | project
	MigratePersona   bool            `json:"migratePersona"`
	RetireGentleKiro bool            `json:"retireGentleKiro"`
	Memory           MemoryAnswers   `json:"memory"`
	Delivery         DeliveryAnswers `json:"delivery"`
	Windows          WindowsAnswers  `json:"windows"`
	DryRun           bool            `json:"dryRun"`
}

// Check is one verification line of a run.
type Check struct {
	Group  string `json:"group"`
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// IssuedToken is a teammate token, shown once and never stored.
type IssuedToken struct {
	User  string `json:"user"`
	Token string `json:"token"`
}

// InstallResult is the closing screen of the wizard and of memory changes.
type InstallResult struct {
	DryRun     bool          `json:"dryRun"`
	Skipped    bool          `json:"skipped"` // nothing to apply (idempotent re-run)
	RolledBack bool          `json:"rolledBack"`
	Applied    []string      `json:"applied"`
	Snapshot   string        `json:"snapshot,omitempty"` // directory of the backup
	Checks     []Check       `json:"checks"`
	Warnings   []string      `json:"warnings"`
	Notices    []Note        `json:"notices"`
	Tokens     []IssuedToken `json:"tokens,omitempty"`
	Steps      []StepResult  `json:"steps"` // persona mode, delivery, code index, Windows
	Error      string        `json:"error,omitempty"`
}

// SignIn is the state of the login to the Git platform.
type SignIn struct {
	Provider string `json:"provider"`
	Known    bool   `json:"known"` // a platform GabitoTL can drive
	OK       bool   `json:"ok"`
	Status   string `json:"status"` // logged-in | not-logged-in | tool-missing | unsupported | unknown
	Command  string `json:"command,omitempty"`
	URL      string `json:"installUrl,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// GuardScan is the result of a guard history audit.
type GuardScan struct {
	Mode     string         `json:"mode"`
	Findings int            `json:"findings"`
	History  []GuardFinding `json:"history"`
}

// GuardFinding is one commit that carries AI attribution.
type GuardFinding struct {
	Commit string `json:"commit"`
	Where  string `json:"where"`
	Text   string `json:"text"`
}
