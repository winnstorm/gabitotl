package vcs

import (
	"errors"
	"fmt"
	"strings"

	"github.com/winnstorm/gabitotl/internal/branding"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// BrandingPolicy is the authorship guard configuration a Repo enforces on the
// text it sends to Git and to pull requests (workflow.md: branding_guard and
// branding_extra_terms). The zero value means the default mode, strip.
type BrandingPolicy struct {
	Mode       string
	ExtraTerms []string
}

// PolicyFromWorkflow builds the policy from workflow.md.
func PolicyFromWorkflow(wf gabitotl.WorkflowConfig) BrandingPolicy {
	return BrandingPolicy{Mode: wf.BrandingGuard, ExtraTerms: wf.BrandingExtraTerms}
}

func (p BrandingPolicy) mode() string {
	if p.Mode == "" {
		return gabitotl.GuardStrip
	}
	return p.Mode
}

// BrandingError is returned when text credits an AI tool as author and the
// guard could not (strip mode: not safely) or must not (block mode) remove it.
// It matches ErrAttribution with errors.Is.
type BrandingError struct {
	Op       string
	Findings []branding.Finding
}

func (e *BrandingError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "vcs: %s refused: the text credits an AI tool as author (authorship belongs to the person who asked for the work):", e.Op)
	for _, f := range e.Findings {
		fmt.Fprintf(&b, " line %d [%s] %q;", f.Line, f.Class, f.Text)
	}
	return strings.TrimSuffix(b.String(), ";")
}

// Is makes errors.Is(err, ErrAttribution) true.
func (e *BrandingError) Is(target error) bool { return target == ErrAttribution }

// Enforce applies the policy to free text (a commit message, a PR title or
// body). It returns the text to use, the lines it removed (strip mode) or only
// reported (warn mode), and a *BrandingError when the text must be refused.
func Enforce(p BrandingPolicy, op, text string) (out string, removed, warned []branding.Finding, err error) {
	mode := p.mode()
	if mode == gabitotl.GuardOff {
		return text, nil, nil, nil
	}
	d := branding.New(p.ExtraTerms...)
	clean, rem, left := d.Strip(text)
	all := append(append([]branding.Finding(nil), rem...), left...)
	switch {
	case len(all) == 0:
		return text, nil, nil, nil
	case mode == gabitotl.GuardWarn:
		return text, nil, all, nil
	case mode == gabitotl.GuardStrip && len(left) == 0:
		return clean, rem, nil, nil
	}
	return text, nil, nil, &BrandingError{Op: op, Findings: all}
}

// IsBranding reports whether err is a BrandingError.
func IsBranding(err error) (*BrandingError, bool) {
	var be *BrandingError
	return be, errors.As(err, &be)
}

// enforce runs Enforce for the repo and reports removals and warnings.
func (r *Repo) enforce(op, text string) (string, error) {
	out, removed, warned, err := Enforce(r.Branding, op, text)
	if err != nil {
		return "", err
	}
	if r.OnBranding != nil && (len(removed) > 0 || len(warned) > 0) {
		r.OnBranding(op, removed, warned)
	}
	return out, nil
}
