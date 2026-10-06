package memory

import (
	"errors"
	"fmt"

	"github.com/winnstorm/gabitotl/internal/branding"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// ErrBranding means a memory entry credits an AI tool as author and the guard
// could not remove it safely (or the mode is block). Memory outlives the chat
// that wrote it, so attribution is kept out of every provider.
var ErrBranding = errors.New("memory: the entry credits an AI tool as author; authorship belongs to the person who asked for the work")

// Policy is the authorship guard configuration for memory text (workflow.md:
// branding_guard and branding_extra_terms). The zero value is the default, strip.
type Policy struct {
	Mode       string
	ExtraTerms []string
}

// PolicyFromWorkflow builds the policy from workflow.md.
func PolicyFromWorkflow(wf gabitotl.WorkflowConfig) Policy {
	return Policy{Mode: wf.BrandingGuard, ExtraTerms: wf.BrandingExtraTerms}
}

// Sanitizer cleans free text fields; it collects what it removed or warned about.
type sanitizer struct {
	p       Policy
	d       *branding.Detector
	removed []branding.Finding
	warned  []branding.Finding
	err     error
}

func (s *sanitizer) field(v string) string {
	if v == "" || s.p.Mode == gabitotl.GuardOff {
		return v
	}
	clean, rem, left := s.d.Strip(v)
	all := append(append([]branding.Finding(nil), rem...), left...)
	switch {
	case len(all) == 0:
		return v
	case s.p.Mode == gabitotl.GuardWarn:
		s.warned = append(s.warned, all...)
		return v
	case s.p.Mode == gabitotl.GuardBlock || len(left) > 0:
		if s.err == nil {
			s.err = fmt.Errorf("%w (line %d, %s)", ErrBranding, all[0].Line, all[0].Class)
		}
		return v
	}
	s.removed = append(s.removed, rem...)
	return clean
}

func (p Policy) sanitizer() *sanitizer { return &sanitizer{p: p, d: branding.New(p.ExtraTerms...)} }

// Sanitize strips attribution lines from the free text of an entry. It returns
// the cleaned entry, what it removed, what it only reported (warn) and an error
// wrapping ErrBranding when the entry must be refused.
func (p Policy) Sanitize(e Entry) (out Entry, removed, warned []branding.Finding, err error) {
	s := p.sanitizer()
	e.Title, e.Content = s.field(e.Title), s.field(e.Content)
	e.Validation, e.NextStep, e.Ojo = s.field(e.Validation), s.field(e.NextStep), s.field(e.Ojo)
	return e, s.removed, s.warned, s.err
}

// SanitizeSummary does the same for a session summary.
func (p Policy) SanitizeSummary(m Summary) (out Summary, removed, warned []branding.Finding, err error) {
	s := p.sanitizer()
	m.Goal, m.Discoveries = s.field(m.Goal), s.field(m.Discoveries)
	m.Accomplished, m.NextSteps = s.field(m.Accomplished), s.field(m.NextSteps)
	return m, s.removed, s.warned, s.err
}

// SanitizePatch does the same for the free text fields of an entry patch.
func (p Policy) SanitizePatch(e EntryPatch) (out EntryPatch, removed, warned []branding.Finding, err error) {
	s := p.sanitizer()
	str := func(v *string) *string {
		if v == nil {
			return nil
		}
		c := s.field(*v)
		return &c
	}
	e.Title, e.Content, e.Validation = str(e.Title), str(e.Content), str(e.Validation)
	e.NextStep, e.Ojo = str(e.NextStep), str(e.Ojo)
	return e, s.removed, s.warned, s.err
}

// SanitizeChange cleans the raw fields of a change record. It runs before
// ChangeEntry, which prefixes each field with a label and would hide a trailer
// that starts its field.
func (p Policy) SanitizeChange(c Change) (out Change, removed, warned []branding.Finding, err error) {
	s := p.sanitizer()
	c.What, c.Why, c.Impact, c.Decisions = s.field(c.What), s.field(c.Why), s.field(c.Impact), s.field(c.Decisions)
	c.Validated, c.Pending, c.Ojo = s.field(c.Validated), s.field(c.Pending), s.field(c.Ojo)
	return c, s.removed, s.warned, s.err
}
