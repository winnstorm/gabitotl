package ui

import (
	"fmt"
	"slices"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// The delivery questions (commit, push, PR, merge) are asked ONCE and saved in
// configuration (docs/DECISIONS.md D33). They are a short fixed sequence shared by
// the plain prompt, the full-screen interface and `gabitotl vcs setup`; nothing
// here ever asks about PR size, budgets, chaining or exceptions.

// DeliveryStep is one delivery question.
type DeliveryStep string

// Delivery questions, in the order they are asked.
const (
	StepPlatform   DeliveryStep = "platform"
	StepAutomation DeliveryStep = "automation"
	StepMerge      DeliveryStep = "merge"
	StepDelete     DeliveryStep = "delete"
	StepDraft      DeliveryStep = "draft"
)

// DeliveryOption is one answer of a delivery question.
type DeliveryOption struct{ Value, Label string }

// NextDeliveryStep returns the next question that still applies ("" when done).
// done lists the steps already asked; an answer already in a (flags) is never
// asked again. Without an automation answer the sequence stops, so ended input
// never settles the questions by guessing.
func NextDeliveryStep(n Need, a gabitotl.DeliveryAnswers, done []DeliveryStep) DeliveryStep {
	if !n.Delivery {
		return ""
	}
	asked := func(s DeliveryStep) bool { return slices.Contains(done, s) }
	if n.DeliveryFull && a.Provider == "" && !asked(StepPlatform) {
		return StepPlatform
	}
	if a.Automation == "" {
		if !asked(StepAutomation) {
			return StepAutomation
		}
		return ""
	}
	if !n.DeliveryFull {
		return ""
	}
	switch {
	case a.Automation == gabitotl.AutomationAll && a.MergeMethod == "" && !asked(StepMerge):
		return StepMerge
	case a.Automation == gabitotl.AutomationAll && a.DeleteBranch == nil && !asked(StepDelete):
		return StepDelete
	case a.Automation != gabitotl.AutomationAsk && a.Draft == nil && !asked(StepDraft):
		return StepDraft
	}
	return ""
}

// platforms are the choices of the platform question; "none" keeps the provider
// that is already set.
var platforms = []string{"github", "gitlab", "bitbucket", "none"}

// DeliveryQuestion returns the title, the options and the index of the default
// option of a step.
func (m Messages) DeliveryQuestion(s DeliveryStep, n Need) (title string, opts []DeliveryOption, def int) {
	opt := func(key string, values ...string) []DeliveryOption {
		out := make([]DeliveryOption, len(values))
		for i, v := range values {
			out[i] = DeliveryOption{v, m.Format(key + "." + v)}
		}
		return out
	}
	switch s {
	case StepPlatform:
		opts, def, title = opt("ask.vcs.platform", platforms...), len(platforms)-1, m.Format("ask.vcs.platform")
		if i := slices.Index(platforms, n.Inferred); i >= 0 && i < len(platforms)-1 {
			def = i
			title = m.Format("ask.vcs.platform.found", "platform", opts[i].Label)
		}
	case StepAutomation:
		return m.Format("ask.vcs.auto"), opt("ask.vcs.auto", gabitotl.AutomationLevels...), 1
	case StepMerge:
		return m.Format("ask.vcs.merge"), opt("ask.vcs.merge", gabitotl.MergeMethods...), 0
	case StepDelete:
		return m.Format("ask.vcs.delete"), opt("ask.vcs.delete", "yes", "no"), 0
	case StepDraft:
		return m.Format("ask.vcs.draft"), opt("ask.vcs.draft", "no", "yes"), 0
	}
	return title, opts, def
}

// ApplyDeliveryAnswer records the chosen value of a step in a.
func ApplyDeliveryAnswer(a *gabitotl.DeliveryAnswers, s DeliveryStep, value string) {
	yes := value == "yes"
	switch s {
	case StepPlatform:
		if value != "none" {
			a.Provider = value
		}
	case StepAutomation:
		a.Automation = value
	case StepMerge:
		a.MergeMethod = value
	case StepDelete:
		a.DeleteBranch = &yes
	case StepDraft:
		a.Draft = &yes
	}
}

// Delivery asks the open delivery questions, one at a time, and fills a. Ended
// input stops the sequence and leaves the rest unanswered.
func (p *Prompter) Delivery(a *gabitotl.DeliveryAnswers, n Need) {
	var done []DeliveryStep
	if n.Delivery && !n.DeliveryFull {
		fmt.Fprintf(p.w, "\n%s %s\n", SymTodo, p.m.Format("ask.vcs.yours"))
	}
	for {
		s := NextDeliveryStep(n, *a, done)
		if s == "" {
			return
		}
		done = append(done, s)
		title, opts, def := p.m.DeliveryQuestion(s, n)
		labels := make([]string, len(opts))
		for i, o := range opts {
			labels[i] = o.Label
		}
		i, ok := p.Choose(title, labels, def)
		if !ok {
			return
		}
		ApplyDeliveryAnswer(a, s, opts[i].Value)
	}
}
