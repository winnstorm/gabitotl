package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// Change is the per-change record of spec section 26.
type Change struct {
	Type      EntryType // defaults to progress
	Project   string
	TopicKey  string
	Actor     string
	Task      string
	Branch    string
	Commit    string
	What      string
	Why       string
	Impact    string
	Files     []string
	Decisions string
	Validated string
	Pending   string
	Ojo       string
	Timestamp time.Time
}

// ChangeEntry renders a Change into an Entry. Empty sections are omitted.
func ChangeEntry(c Change) Entry {
	t := c.Type
	if t == "" {
		t = TypeProgress
	}
	var b strings.Builder
	section := func(label, v string) {
		if strings.TrimSpace(v) != "" {
			fmt.Fprintf(&b, "**%s**: %s\n", label, strings.TrimSpace(v))
		}
	}
	section("What", c.What)
	section("Why", c.Why)
	section("Impact", c.Impact)
	section("Decisions", c.Decisions)
	return Entry{
		Type: t, Title: firstLine(c.What), Content: strings.TrimSpace(b.String()),
		Project: c.Project, TopicKey: c.TopicKey, Actor: c.Actor, Task: c.Task,
		Branch: c.Branch, Commit: c.Commit, Files: c.Files, Validation: c.Validated,
		NextStep: c.Pending, Ojo: c.Ojo, Timestamp: c.Timestamp,
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 100 {
		s = string(r[:100])
	}
	return s
}

// SessionEntry renders a Summary as a progress Entry. It is the fallback for
// providers that cannot store session summaries natively.
func SessionEntry(s Summary) Entry {
	var b strings.Builder
	section := func(label, v string) {
		if strings.TrimSpace(v) != "" {
			fmt.Fprintf(&b, "**%s**: %s\n", label, strings.TrimSpace(v))
		}
	}
	section("Goal", s.Goal)
	section("Discoveries", s.Discoveries)
	section("Accomplished", s.Accomplished)
	section("Next steps", s.NextSteps)
	title := "Session summary"
	if s.Task != "" {
		title += ": " + s.Task
	}
	return Entry{
		Type: TypeProgress, Title: title, Content: strings.TrimSpace(b.String()),
		Project: s.Project, Actor: s.Actor, Task: s.Task, Branch: s.Branch,
		Files: s.Files, NextStep: s.NextSteps, Timestamp: s.Timestamp,
		TopicKey: "session/summary",
	}
}

// Recorder saves after-work entries through a Provider and honors
// workflow.memory_after_work.
type Recorder struct {
	Provider Provider
	Enabled  bool
}

// NewRecorder builds a Recorder from the workflow configuration.
func NewRecorder(p Provider, wf gabitotl.WorkflowConfig) Recorder {
	return Recorder{Provider: p, Enabled: wf.MemoryAfterWork}
}

// RecordChange saves the change record. recorded is false (with a nil error)
// when after-work memory is disabled.
func (r Recorder) RecordChange(ctx context.Context, c Change) (id ID, recorded bool, err error) {
	if !r.Enabled {
		return "", false, nil
	}
	id, err = r.Provider.Save(ctx, ChangeEntry(c))
	return id, err == nil, err
}

// RecordSession stores the session summary. When the provider has no native
// session summary (ErrUnsupported) it falls back to a progress entry.
func (r Recorder) RecordSession(ctx context.Context, s Summary) (recorded bool, err error) {
	if !r.Enabled {
		return false, nil
	}
	err = r.Provider.SessionSummary(ctx, s)
	if errors.Is(err, ErrUnsupported) {
		_, err = r.Provider.Save(ctx, SessionEntry(s))
	}
	return err == nil, err
}
