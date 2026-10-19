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
// detected is ever asked (never runtime, Engram or Git itself).
type Need struct {
	Name, Mode bool
	// PersonaLang asks the language of Gabito's chat voice (never asked again once
	// chosen or given with --persona-lang).
	PersonaLang bool
	// Delivery means the delivery settings (commit, push, PR, merge) were never
	// answered for this project. DeliveryFull asks the whole short sequence; without
	// it only the person's automation is asked (the team already answered the rest).
	// Inferred is the platform found in origin, offered as the default.
	Delivery, DeliveryFull bool
	Inferred               string
	// Current is the provider the project already uses when Mode is asked about a
	// project that exists: the question then offers to change it (to team memory)
	// instead of choosing one.
	Current string
	// Team means the team setup (role, server URL, token variable) is still open, so
	// it is asked whenever the answers lead to team memory. CanImport adds the
	// question about importing the existing Engram memory (host role only).
	Team, CanImport bool
	// Pending means the project already chose team memory but memory.md has no
	// server yet, so the team setup is asked to finish it.
	Pending bool
	// Teammate means the committed memory.md already names the team server and
	// nothing is configured for this person yet (right after `git pull`): the role is
	// client, the url is SharedURL and only the token variable is asked.
	Teammate  bool
	SharedURL string
}

// Onboard asks the open questions, one at a time, and fills opts.
func (p *Prompter) Onboard(opts *installer.Options, n Need, suggestedName string) {
	if n.Name {
		name := p.Ask(p.m.Format("ask.name"), suggestedName)
		if name != "" {
			opts.UserName = name
		}
	}
	if n.PersonaLang {
		if i, ok := p.Choose(p.m.Format("ask.lang"), []string{p.m.Format("ask.lang.es"), p.m.Format("ask.lang.en")}, 0); ok && i == 1 {
			opts.PersonaLang = "en"
		}
	}
	if n.Mode {
		question, solo, team := "ask.mode", "ask.mode.solo", "ask.mode.team"
		if n.Current != "" {
			question, solo, team = "ask.change", "ask.change.no", "ask.change.yes"
		}
		if i, ok := p.Choose(p.m.Format(question), []string{p.m.Format(solo), p.m.Format(team)}, 0); ok && i == 1 {
			opts.Mode, opts.Memory = "team", "memory-mcp"
		}
	}
	if n.Pending && opts.Memory == "" {
		opts.Mode, opts.Memory = "team", "memory-mcp"
	}
	if n.Teammate {
		p.askTeammate(opts, n)
	} else if n.Team && opts.Memory == "memory-mcp" {
		p.askTeam(opts, n)
	}
	if n.Delivery {
		p.Delivery(&opts.Delivery, n)
	}
}

// askTeammate finishes the setup of a teammate: the server is already in the
// committed memory.md, so neither the role nor the url is asked, only the name of
// the variable that will hold this person's token.
func (p *Prompter) askTeammate(opts *installer.Options, n Need) {
	if opts.MemoryRole != "" || opts.MemoryURL != "" {
		return // flags answer it
	}
	opts.MemoryRole, opts.MemoryURL = "client", n.SharedURL
	fmt.Fprintf(p.w, "%s %s\n", SymOK, p.m.Format("ask.team.known", "url", n.SharedURL))
	p.askTokenEnv(opts)
}

// askTokenEnv asks for the NAME of the token variable unless a flag gave it.
func (p *Prompter) askTokenEnv(opts *installer.Options) {
	for opts.MemoryTokenEnv == "" {
		env := p.Ask(p.m.Format("ask.team.tokenenv"), installer.DefaultTokenEnv)
		if installer.ValidTokenEnv(env) {
			opts.MemoryTokenEnv = env
			return
		}
		fmt.Fprintf(p.w, "%s %s\n", SymWarn, p.m.Format("ask.team.badenv"))
	}
}

// askTeam asks what the existing team setup needs and nothing more: the role of
// this machine, then the server URL and token variable for a client, or the URL
// teammates use and whether to import the Engram memory for a host. Answers given by flag are not asked
// again, and ended input leaves the team setup undecided rather than guessing.
func (p *Prompter) askTeam(opts *installer.Options, n Need) {
	if opts.MemoryRole == "" && opts.MemoryURL == "" {
		i, ok := p.Choose(p.m.Format("ask.team.role"), []string{p.m.Format("ask.team.client"), p.m.Format("ask.team.host"), p.m.Format("ask.team.stay")}, 0)
		switch {
		case !ok || i == 2:
			opts.Mode, opts.Memory = "", ""
			return
		case i == 0:
			opts.MemoryRole = "client"
		default:
			opts.MemoryRole = "host"
		}
	}
	if opts.MemoryRole == "client" {
		for opts.MemoryURL == "" {
			url := p.Ask(p.m.Format("ask.team.url"), "")
			if url == "" {
				return // the plan reports the missing URL; nothing is guessed
			}
			if err := installer.ValidateTeamURL(url); err != nil {
				fmt.Fprintf(p.w, "%s %s\n", SymWarn, p.m.Format("ask.team.badurl", "detail", err.Error()))
				continue
			}
			opts.MemoryURL = url
		}
		p.askTokenEnv(opts)
	}
	if opts.MemoryRole == "host" && opts.MemoryURL == "" {
		// The address teammates use goes into the committed file; it may stay empty.
		for {
			url := p.Ask(p.m.Format("ask.team.hosturl"), "")
			if url == "" {
				break
			}
			if err := installer.ValidateTeamHostURL(url); err != nil {
				fmt.Fprintf(p.w, "%s %s\n", SymWarn, p.m.Format("ask.team.hostbadurl", "detail", err.Error()))
				continue
			}
			opts.MemoryURL = url
			break
		}
	}
	if opts.MemoryRole == "host" && n.CanImport && !opts.ImportEngram {
		opts.ImportEngram = p.Confirm(p.m.Format("ask.team.import"))
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
		}
	}
}
