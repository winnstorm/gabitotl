package store

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/winnstorm/gabitotl/internal/memory"
)

const (
	maxSearchTerms = 12
	maxTermRunes   = 64
)

// Terms splits free text into lowercase search terms: runs of letters and
// digits. Both implementations tokenize the same way so they rank alike
// (PostgreSQL's `simple` configuration does not stem, so neither do we).
func Terms(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if r := []rune(w); len(r) > maxTermRunes {
			w = string(r[:maxTermRunes])
		}
		if seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
		if len(out) == maxSearchTerms {
			break
		}
	}
	return out
}

// CheckEntry is the write-side validation every implementation applies.
func CheckEntry(e memory.Entry) error {
	if strings.TrimSpace(e.Project) == "" {
		return fmt.Errorf("%w: project is required", ErrInvalid)
	}
	if strings.TrimSpace(e.Actor) == "" {
		return fmt.Errorf("%w: %v", ErrInvalid, memory.ErrActorRequired)
	}
	if err := e.Validate(""); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if hasNUL(e.Project, string(e.Type), e.Title, e.Content, e.TopicKey, e.Actor, e.Task, e.Branch, e.Commit,
		e.Validation, e.NextStep, e.Ojo, e.Source) || hasNUL(e.Files...) {
		return fmt.Errorf("%w: text must not contain NUL characters", ErrInvalid)
	}
	return nil
}

func hasNUL(vals ...string) bool {
	for _, v := range vals {
		if strings.ContainsRune(v, 0) {
			return true
		}
	}
	return false
}

// CheckSummary validates a session summary.
func CheckSummary(s memory.Summary) error {
	if strings.TrimSpace(s.Project) == "" {
		return fmt.Errorf("%w: project is required", ErrInvalid)
	}
	if strings.TrimSpace(s.Actor) == "" {
		return fmt.Errorf("%w: %v", ErrInvalid, memory.ErrActorRequired)
	}
	if hasNUL(s.Project, s.Actor, s.Task, s.Branch, s.Goal, s.Discoveries, s.Accomplished, s.NextSteps) || hasNUL(s.Files...) {
		return fmt.Errorf("%w: text must not contain NUL characters", ErrInvalid)
	}
	return nil
}

// ApplyPatch returns e with the patch applied (nil fields are kept) and the
// validation of the result.
func ApplyPatch(e memory.Entry, p memory.EntryPatch) (memory.Entry, error) {
	if p.Title != nil {
		e.Title = *p.Title
	}
	if p.Content != nil {
		e.Content = *p.Content
	}
	if p.Type != nil {
		e.Type = *p.Type
	}
	if p.Task != nil {
		e.Task = *p.Task
	}
	if p.Branch != nil {
		e.Branch = *p.Branch
	}
	if p.Commit != nil {
		e.Commit = *p.Commit
	}
	if p.Files != nil {
		e.Files = append([]string(nil), p.Files...)
	}
	if p.Validation != nil {
		e.Validation = *p.Validation
	}
	if p.NextStep != nil {
		e.NextStep = *p.NextStep
	}
	if p.Ojo != nil {
		e.Ojo = *p.Ojo
	}
	if err := CheckPatched(e); err != nil {
		return e, err
	}
	return e, nil
}

// CheckPatched validates an entry after a patch (project and actor already exist).
func CheckPatched(e memory.Entry) error {
	if err := e.Validate(""); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if hasNUL(string(e.Type), e.Title, e.Content, e.Task, e.Branch, e.Commit, e.Validation, e.NextStep, e.Ojo) || hasNUL(e.Files...) {
		return fmt.Errorf("%w: text must not contain NUL characters", ErrInvalid)
	}
	return nil
}
