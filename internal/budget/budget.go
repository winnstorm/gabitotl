// Package budget watches the size of a working session (spec section 20).
// When the session nears its context budget it asks the caller to save state
// to memory and produces the TIP that tells the user they can continue in a new
// session. It never blocks or forces anything on the user.
package budget

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
	"unicode/utf8"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/persona"
)

// Defaults.
const (
	DefaultLimit     = 200_000 // tokens of context budget
	DefaultThreshold = 0.75    // fraction of the limit that triggers the checkpoint
)

// Token sources.
const (
	SourceReported  = "reported"  // real count supplied by the runtime
	SourceEstimated = "estimated" // chars/4 heuristic
)

// EstimateTokens estimates tokens as ceil(characters / 4), counting runes. This
// is the usual rule of thumb for English prose and code; Spanish and non-ASCII
// text tokenize a bit worse, so the estimate leans slightly low. It is only
// used when the runtime does not report real token counts.
func EstimateTokens(s string) int {
	return (utf8.RuneCountInString(s) + 3) / 4
}

// Config sets the context budget.
type Config struct {
	Limit     int     // tokens
	Threshold float64 // 0 < Threshold <= 1
}

// DefaultConfig returns the default limit and the 75% threshold.
func DefaultConfig() Config { return Config{Limit: DefaultLimit, Threshold: DefaultThreshold} }

// FromWorkflow reads the budget from workflow.md (context_limit_tokens and
// context_threshold). Zero values fall back to the defaults, so configs that
// predate these keys keep working.
func FromWorkflow(w gabitotl.WorkflowConfig) Config {
	c := DefaultConfig()
	if w.ContextLimitTokens > 0 {
		c.Limit = w.ContextLimitTokens
	}
	if w.ContextThreshold > 0 {
		c.Threshold = w.ContextThreshold
	}
	return c
}

// Validate checks the configuration.
func (c Config) Validate() error {
	if c.Limit <= 0 {
		return errors.New("budget: limit must be positive")
	}
	if c.Threshold <= 0 || c.Threshold > 1 {
		return errors.New("budget: threshold must be in (0, 1]")
	}
	return nil
}

// sessionState is persisted in .gabitotl/local/session.md (never committed).
type sessionState struct {
	Started     string `yaml:"started"`
	UsedTokens  int    `yaml:"used_tokens"`
	TokenSource string `yaml:"token_source"`
	Notified    bool   `yaml:"budget_notified"` // the checkpoint already fired this session
	Checkpoint  string `yaml:"checkpoint_at,omitempty"`
}

// Decision is the outcome of Check.
type Decision struct {
	ShouldSave bool    `json:"should_save"`
	Message    string  `json:"message,omitempty"` // the TIP to show once state is saved
	Used       int     `json:"used"`
	Limit      int     `json:"limit"`
	Percent    float64 `json:"percent"`
	Source     string  `json:"source"`
}

// Monitor tracks session size. Create it with New; it is not safe for
// concurrent use.
type Monitor struct {
	Layout gabitotl.Layout
	Config Config
	Msgs   persona.Messages
	// Now is the clock; nil means time.Now.
	Now func() time.Time

	state sessionState
}

// New loads the session state (if any) and returns a Monitor.
func New(layout gabitotl.Layout, cfg Config) (*Monitor, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	m := &Monitor{Layout: layout, Config: cfg, Msgs: persona.DefaultMessages()}
	d, err := gabitotl.ReadDoc(layout.LocalSession())
	switch {
	case err == nil:
		if d.Decode(&m.state) != nil {
			m.state = sessionState{} // unreadable state starts a fresh session
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	if m.state.Started == "" {
		m.state.Started = m.now().UTC().Format(time.RFC3339)
	}
	return m, nil
}

func (m *Monitor) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Monitor) save() error {
	return gabitotl.WriteDoc(m.Layout.LocalSession(), m.state, "\n# Session\n\nLocal session state. Not committed.\n")
}

// NewSession resets the counters and re-arms the checkpoint.
func (m *Monitor) NewSession() error {
	m.state = sessionState{Started: m.now().UTC().Format(time.RFC3339)}
	return m.save()
}

// AddText adds the estimated size of text to the session. Ignored once the
// runtime has reported real counts.
func (m *Monitor) AddText(text string) error {
	if m.state.TokenSource == SourceReported {
		return nil
	}
	m.state.UsedTokens += EstimateTokens(text)
	m.state.TokenSource = SourceEstimated
	return m.save()
}

// ReportTokens records the real token count of the session as reported by the
// runtime. From now on estimates are ignored.
func (m *Monitor) ReportTokens(used int) error {
	if used < 0 {
		return errors.New("budget: token count must not be negative")
	}
	m.state.UsedTokens = used
	m.state.TokenSource = SourceReported
	return m.save()
}

// Used returns the current token count and its source.
func (m *Monitor) Used() (int, string) {
	src := m.state.TokenSource
	if src == "" {
		src = SourceEstimated
	}
	return m.state.UsedTokens, src
}

// Check decides whether the session should save its state now. It reports
// ShouldSave=true exactly once per session, when usage first reaches the
// threshold; later calls report false until NewSession.
func (m *Monitor) Check() (Decision, error) {
	used, src := m.Used()
	d := Decision{Used: used, Limit: m.Config.Limit, Source: src, Percent: float64(used) / float64(m.Config.Limit) * 100}
	if m.state.Notified || float64(used) < m.Config.Threshold*float64(m.Config.Limit) {
		return d, nil
	}
	m.state.Notified = true
	if err := m.save(); err != nil {
		m.state.Notified = false
		return d, err
	}
	d.ShouldSave = true
	d.Message = persona.Renderer{Msgs: m.Msgs}.TipLine(m.Msgs.ContextTip())
	return d, nil
}

// State is what a checkpoint stores in memory.
type State struct {
	Project   string
	Actor     string
	Task      string
	Branch    string
	Goal      string
	Progress  string   // what is done so far
	NextStep  string   // "Qué sigue"
	Decisions []string // new decisions of this session
	Files     []string
}

// CheckpointResult reports what a checkpoint did.
type CheckpointResult struct {
	Recorded bool     // false when after-work memory is disabled
	Message  string   // the TIP for the user; empty when nothing was recorded
	Warnings []string // for example a missing next step
}

// Checkpoint saves decisions, progress, the next step and a session summary
// through the recorder and returns the TIP for the user. It only claims state
// was saved when memory actually recorded it. If saving fails the checkpoint is
// re-armed so the next Check retries; the error is for logging, the user's
// work is never blocked.
func (m *Monitor) Checkpoint(ctx context.Context, rec memory.Recorder, st State) (CheckpointResult, error) {
	var res CheckpointResult
	if st.NextStep == "" {
		res.Warnings = append(res.Warnings, "no next step provided; 'Qué sigue' was not recorded")
	}
	change := memory.Change{
		Type: memory.TypeProgress, Project: st.Project, TopicKey: "session/checkpoint",
		Actor: st.Actor, Task: st.Task, Branch: st.Branch,
		What: orDefault(st.Progress, "Session checkpoint"), Files: st.Files, Pending: st.NextStep,
		Timestamp: m.now(),
	}
	if len(st.Decisions) > 0 {
		change.Decisions = joinLines(st.Decisions)
	}
	_, recorded, err := rec.RecordChange(ctx, change)
	if err == nil && recorded {
		_, err = rec.RecordSession(ctx, memory.Summary{
			Project: st.Project, Actor: st.Actor, Task: st.Task, Branch: st.Branch, Goal: st.Goal,
			Accomplished: st.Progress, NextSteps: st.NextStep, Files: st.Files, Timestamp: m.now(),
		})
	}
	if err != nil {
		m.state.Notified = false
		_ = m.save()
		return res, fmt.Errorf("budget: checkpoint not saved: %w", err)
	}
	res.Recorded = recorded
	if !recorded {
		return res, nil
	}
	m.state.Checkpoint = m.now().UTC().Format(time.RFC3339)
	if err := m.save(); err != nil {
		return res, err
	}
	res.Message = persona.Renderer{Msgs: m.Msgs}.TipLine(m.Msgs.ContextTip())
	return res, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func joinLines(items []string) string {
	out := ""
	for i, it := range items {
		if i > 0 {
			out += "; "
		}
		out += it
	}
	return out
}
