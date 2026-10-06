package ui

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/winnstorm/gabitotl/internal/installer"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// Prompter asks one question at a time on plain stdin/stdout (no TUI: Bubble
// Tea/Huh are deferred, see docs/ROADMAP.md). End of input never answers for
// the user: Ask returns its default, Confirm declines, Choose reports !ok.
type Prompter struct {
	r *bufio.Reader
	w io.Writer
	m Messages
}

// NewPrompter builds a Prompter.
func NewPrompter(in io.Reader, out io.Writer, m Messages) *Prompter {
	return &Prompter{r: bufio.NewReader(in), w: out, m: m}
}

func (p *Prompter) read() (string, bool) {
	s, err := p.r.ReadString('\n')
	if err != nil && s == "" {
		return "", false
	}
	return strings.TrimSpace(s), true
}

// Ask prints a one-line question and returns the answer, or def when empty.
func (p *Prompter) Ask(question, def string) string {
	if def != "" {
		fmt.Fprintf(p.w, "%s [%s]\n> ", question, def)
	} else {
		fmt.Fprintf(p.w, "%s\n> ", question)
	}
	if s, ok := p.read(); ok && s != "" {
		return s
	}
	return def
}

// Choose shows a numbered list and returns the 0-based index chosen. An empty
// answer picks def (when def >= 0). ok is false when input ended.
func (p *Prompter) Choose(question string, labels []string, def int) (idx int, ok bool) {
	for {
		fmt.Fprintf(p.w, "\n%s\n", question)
		for i, l := range labels {
			fmt.Fprintf(p.w, "  %d) %s\n", i+1, l)
		}
		prompt := p.m.Format("ask.choose")
		if def >= 0 {
			prompt += " [" + strconv.Itoa(def+1) + "]"
		}
		fmt.Fprintf(p.w, "%s: ", prompt)
		s, got := p.read()
		if !got {
			return -1, false
		}
		if s == "" && def >= 0 {
			return def, true
		}
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= len(labels) {
			return n - 1, true
		}
		fmt.Fprintf(p.w, "%s %s\n", SymWarn, p.m.Format("ask.invalid"))
	}
}

// Confirm asks a yes/no question; anything but an explicit yes (s, si, sí, y,
// yes) declines, and so does end of input.
func (p *Prompter) Confirm(question string) bool {
	fmt.Fprintf(p.w, "%s %s ", question, p.m.Format("ask.yesno"))
	s, ok := p.read()
	if !ok {
		return false
	}
	switch strings.ToLower(s) {
	case "s", "si", "sí", "y", "yes":
		return true
	}
	return false
}

// Need says which onboarding questions are still open: only what cannot be
// detected is ever asked (never runtime, Engram, Git or provider).
type Need struct{ Name, Mode, Git bool }

// Onboard asks the open questions, one at a time, and fills opts.
func (p *Prompter) Onboard(opts *installer.Options, n Need, suggestedName string) {
	if n.Name {
		name := p.Ask(p.m.Format("ask.name"), suggestedName)
		if name != "" {
			opts.UserName = name
		}
	}
	if n.Mode {
		if i, ok := p.Choose(p.m.Format("ask.mode"), []string{p.m.Format("ask.mode.solo"), p.m.Format("ask.mode.team")}, 0); ok && i == 1 {
			opts.Mode = "team"
		}
	}
	if n.Git {
		if i, ok := p.Choose(p.m.Format("ask.git"), []string{p.m.Format("ask.git.yes"), p.m.Format("ask.git.no")}, 1); ok && i == 0 {
			opts.AutomateGit = true
		}
	}
}

// Action is the answer of the "what do you want to do" menu (spec 28).
type Action int

// Menu answers.
const (
	ActionMigrate Action = iota
	ActionKeep
	ActionExit
)

// ChooseAction shows the Gentle-AI menu; "review details" prints the inventory
// and asks again. Ended input exits without choosing for the user.
func (p *Prompter) ChooseAction(inv installer.Inventory) Action {
	for {
		i, ok := p.Choose(p.m.Format("ask.action"), []string{
			p.m.Format("ask.action.migrate"), p.m.Format("ask.action.keep"),
			p.m.Format("ask.action.inventory"), p.m.Format("ask.action.exit"),
		}, 0)
		switch {
		case !ok || i == 3:
			return ActionExit
		case i == 0:
			return ActionMigrate
		case i == 1:
			return ActionKeep
		}
		RenderInventory(p.w, p.m, inv)
	}
}

// ConfirmPersona shows exactly which managed persona blocks would be removed
// and asks for consent. The blocks are removed only after verification.
func (p *Prompter) ConfirmPersona(blocks []rt.Action, paths Paths) bool {
	if len(blocks) == 0 {
		return false
	}
	fmt.Fprintf(p.w, "\n%s %s\n", SymWarn, p.m.Format("ask.persona", "detail", blocks[0].Runtime))
	fmt.Fprintf(p.w, "%s\n", p.m.Format("ask.persona.show"))
	for _, b := range blocks {
		fmt.Fprintf(p.w, "  %s %s: %s\n", SymTodo, b.Runtime, paths.Short(b.Path)+" ["+b.Target+"]")
	}
	return p.Confirm(p.m.Format("ask.persona.confirm"))
}

// ConfirmApply asks before applying the plan.
func (p *Prompter) ConfirmApply() bool { return p.Confirm(p.m.Format("ask.apply")) }

// RetireHook is the interactive "retire Gentle-AI?" question, asked only after
// every verification passed. "Review" shows what would run and asks again.
func (p *Prompter) RetireHook() func(installer.RetirePreview) bool {
	return func(pv installer.RetirePreview) bool {
		fmt.Fprintf(p.w, "\n%s\n", p.m.Format("ask.retire.title"))
		for {
			i, ok := p.Choose("", []string{p.m.Format("ask.retire.do"), p.m.Format("ask.retire.keep"), p.m.Format("ask.retire.review")}, 1)
			switch {
			case !ok || i == 1:
				return false
			case i == 0:
				return true
			}
			fmt.Fprintf(p.w, "%s %s\n", SymTodo, p.m.Format("ask.retire.preview", "detail", strings.Join(pv.Args, " ")))
			if !pv.Covered {
				fmt.Fprintf(p.w, "%s %s\n", SymWarn, p.m.Format("ask.retire.narrow"))
			}
		}
	}
}
