// Package memory is the provider-agnostic memory contract. The workflow
// depends on Provider only: concrete providers (Engram CLI, Memory MCP) live in
// sub-packages and are selected through a Factory at the composition root.
package memory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/branding"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// EntryType classifies a memory entry.
type EntryType string

// Entry types (spec section 26 and the memory rules).
const (
	TypeDecision   EntryType = "decision"
	TypeBugfix     EntryType = "bugfix"
	TypeDiscovery  EntryType = "discovery"
	TypeConvention EntryType = "convention"
	TypeConfig     EntryType = "config"
	TypePreference EntryType = "preference"
	TypePending    EntryType = "pending"
	TypeRisk       EntryType = "risk"
	TypeProgress   EntryType = "progress"
	TypeProjectMap EntryType = "project-map"
)

// EntryTypes lists every valid type.
var EntryTypes = []EntryType{
	TypeDecision, TypeBugfix, TypeDiscovery, TypeConvention, TypeConfig,
	TypePreference, TypePending, TypeRisk, TypeProgress, TypeProjectMap,
}

// ActorLegacy marks historical or unknown authorship. An actor is never invented.
const ActorLegacy = "legacy"

// DefaultSource is stamped on entries that do not name a source.
const DefaultSource = "gabitotl"

// Errors shared by every provider.
var (
	// ErrUnsupported is returned when a provider has no equivalent operation.
	ErrUnsupported = errors.New("memory: operation not supported by this provider")
	// ErrActorRequired is returned when a team-mode entry has no actor.
	ErrActorRequired = errors.New("memory: actor is required in team mode (use \"legacy\" for historical or unknown authorship)")
)

// ID identifies a stored entry within its provider.
type ID string

// Entry is one memory record with the traceability fields of spec sections 7 and 26.
type Entry struct {
	ID         ID        `json:"id,omitempty"`
	Type       EntryType `json:"type"`
	Title      string    `json:"title"`
	Content    string    `json:"content"`
	Project    string    `json:"project,omitempty"`
	TopicKey   string    `json:"topic_key,omitempty"`
	Actor      string    `json:"actor,omitempty"`
	Task       string    `json:"task,omitempty"`
	Branch     string    `json:"branch,omitempty"`
	Commit     string    `json:"commit,omitempty"`
	Files      []string  `json:"files,omitempty"`
	Validation string    `json:"validation,omitempty"`
	NextStep   string    `json:"next_step,omitempty"`
	Ojo        string    `json:"ojo,omitempty"`
	Timestamp  time.Time `json:"timestamp,omitzero"`
	Source     string    `json:"source,omitempty"`
}

// Validate checks the entry for a memory mode ("solo" or "team").
func (e Entry) Validate(mode string) error {
	if !slices.Contains(EntryTypes, e.Type) {
		return fmt.Errorf("memory: invalid entry type %q", e.Type)
	}
	if strings.TrimSpace(e.Title) == "" {
		return errors.New("memory: title is required")
	}
	if strings.TrimSpace(e.Content) == "" {
		return errors.New("memory: content is required")
	}
	if mode == gabitotl.ModeTeam && strings.TrimSpace(e.Actor) == "" {
		return ErrActorRequired
	}
	return nil
}

// EntryPatch updates selected fields of a stored entry; nil fields are kept.
type EntryPatch struct {
	Title      *string    `json:"title,omitempty"`
	Content    *string    `json:"content,omitempty"`
	Type       *EntryType `json:"type,omitempty"`
	Task       *string    `json:"task,omitempty"`
	Branch     *string    `json:"branch,omitempty"`
	Commit     *string    `json:"commit,omitempty"`
	Files      []string   `json:"files,omitempty"`
	Validation *string    `json:"validation,omitempty"`
	NextStep   *string    `json:"next_step,omitempty"`
	Ojo        *string    `json:"ojo,omitempty"`
}

// Query is a relevance-oriented search. Memory is never loaded in full.
type Query struct {
	Text     string    `json:"text"`
	Project  string    `json:"project,omitempty"`
	Type     EntryType `json:"type,omitempty"`
	Limit    int       `json:"limit,omitempty"`
	MatchAny bool      `json:"match_any,omitempty"`
}

// Summary closes a working session.
type Summary struct {
	Project      string    `json:"project,omitempty"`
	Actor        string    `json:"actor,omitempty"`
	Task         string    `json:"task,omitempty"`
	Branch       string    `json:"branch,omitempty"`
	Goal         string    `json:"goal,omitempty"`
	Discoveries  string    `json:"discoveries,omitempty"`
	Accomplished string    `json:"accomplished,omitempty"`
	NextSteps    string    `json:"next_steps,omitempty"`
	Files        []string  `json:"files,omitempty"`
	Timestamp    time.Time `json:"timestamp,omitzero"`
}

// Check statuses.
const (
	StatusOK    = "ok"
	StatusWarn  = "warning"
	StatusError = "error"
)

// Check is one diagnostic finding.
type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// Report is the read-only result of Provider.Doctor.
type Report struct {
	Provider string  `json:"provider"`
	Project  string  `json:"project,omitempty"`
	Checks   []Check `json:"checks"`
}

// OK reports whether no check is an error.
func (r Report) OK() bool {
	for _, c := range r.Checks {
		if c.Status == StatusError {
			return false
		}
	}
	return true
}

// Provider is the only memory surface the workflow may depend on.
type Provider interface {
	Search(ctx context.Context, q Query) ([]Entry, error)
	Save(ctx context.Context, e Entry) (ID, error)
	Update(ctx context.Context, id ID, patch EntryPatch) error
	SessionSummary(ctx context.Context, s Summary) error
	CurrentProject(ctx context.Context) (string, error)
	Doctor(ctx context.Context) Report
}

// Guard decorates a Provider so every Save is validated for the memory mode and
// stamped with a timestamp and source. Factory.New returns guarded providers.
type Guard struct {
	Provider
	Mode string
	Now  func() time.Time
	// Branding is the authorship guard for entry text; the zero value strips
	// attribution lines (store-level defense in depth, see Policy).
	Branding Policy
	// OnBranding is told what was removed or only reported. May be nil.
	OnBranding func(removed, warned []branding.Finding)
}

func (g Guard) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now().UTC()
}

// Save validates, stamps and forwards the entry.
func (g Guard) Save(ctx context.Context, e Entry) (ID, error) {
	e, rem, warned, err := g.Branding.Sanitize(e)
	if err != nil {
		return "", err
	}
	if g.OnBranding != nil && len(rem)+len(warned) > 0 {
		g.OnBranding(rem, warned)
	}
	if err := e.Validate(g.Mode); err != nil {
		return "", err
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = g.now()
	}
	if e.Source == "" {
		e.Source = DefaultSource
	}
	return g.Provider.Save(ctx, e)
}

// Update cleans the patch the same way Save cleans an entry.
func (g Guard) Update(ctx context.Context, id ID, patch EntryPatch) error {
	patch, rem, warned, err := g.Branding.SanitizePatch(patch)
	if err != nil {
		return err
	}
	if g.OnBranding != nil && len(rem)+len(warned) > 0 {
		g.OnBranding(rem, warned)
	}
	return g.Provider.Update(ctx, id, patch)
}

// SessionSummary enforces the team-mode actor rule and stamps the time.
func (g Guard) SessionSummary(ctx context.Context, s Summary) error {
	if g.Mode == gabitotl.ModeTeam && strings.TrimSpace(s.Actor) == "" {
		return ErrActorRequired
	}
	s, rem, warned, err := g.Branding.SanitizeSummary(s)
	if err != nil {
		return err
	}
	if g.OnBranding != nil && len(rem)+len(warned) > 0 {
		g.OnBranding(rem, warned)
	}
	if s.Timestamp.IsZero() {
		s.Timestamp = g.now()
	}
	return g.Provider.SessionSummary(ctx, s)
}

// Constructor builds a concrete provider from the project memory configuration.
type Constructor func(cfg gabitotl.MemoryConfig) (Provider, error)

// Factory selects a provider from gabitotl.MemoryConfig. Concrete providers are
// registered by the composition root, so core code never imports them.
type Factory struct {
	ctors map[string]Constructor
}

// NewFactory returns an empty factory.
func NewFactory() *Factory { return &Factory{ctors: map[string]Constructor{}} }

// Register binds a provider name (MemoryConfig.Provider) to a constructor.
func (f *Factory) Register(name string, c Constructor) { f.ctors[name] = c }

// Names lists registered providers in sorted order.
func (f *Factory) Names() []string {
	names := make([]string, 0, len(f.ctors))
	for n := range f.ctors {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// New builds the configured provider wrapped in a Guard for cfg.Mode.
func (f *Factory) New(cfg gabitotl.MemoryConfig) (Provider, error) {
	c, ok := f.ctors[cfg.Provider]
	if !ok {
		return nil, fmt.Errorf("memory: no provider registered for %q (have %v)", cfg.Provider, f.Names())
	}
	p, err := c(cfg)
	if err != nil {
		return nil, err
	}
	return Guard{Provider: p, Mode: cfg.Mode}, nil
}
