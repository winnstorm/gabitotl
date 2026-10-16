package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// Config is everything the TUI is given; it decides nothing by itself.
type Config struct {
	Backend  Backend
	Messages ui.Messages
	Paths    ui.Paths
	// Options are the decisions already taken from flags; they are never asked again.
	Options installer.Options
	// Need says which onboarding questions are still open for this inventory
	// (only what cannot be detected). Nil asks nothing.
	Need func(inv installer.Inventory, opts installer.Options) ui.Need
	// SuggestedName prefills the display-name question.
	SuggestedName string
	NoColor       bool
	// Timeout bounds the apply phase (default 10 minutes).
	Timeout time.Duration
}

// Outcome is what the caller needs after the program ends.
type Outcome struct {
	Ran       bool // the pipeline was started (Result and Err are meaningful)
	Result    installer.Result
	Err       error
	Cancelled bool // the user cancelled while applying
	Inv       installer.Inventory
	Source    string
}

type screen int

const (
	scrDetect screen = iota
	scrHome
	scrDetails
	scrMemory
	scrTeam
	scrEngram
	scrForm
	scrPersona
	scrGentleKiro
	scrBuilding
	scrPlan
	scrApply
	scrRetire
	scrResult
)

// Onboarding stages, in order. A stage that does not apply is skipped.
type stage int

const (
	stMemory stage = iota
	stName
	stMode
	stTeam
	stEngram
	stGit
	stRuntimes
	stPlan
)

// Messages between the pipeline goroutine and the model.
type (
	detectedMsg struct{ inv installer.Inventory }
	planMsg     struct {
		plan    installer.Plan
		err     error
		preview bool // the plan with the persona block retired, to show what goes
	}
	eventMsg     installer.Event
	retireAskMsg struct {
		preview installer.RetirePreview
		reply   chan bool
	}
	doneMsg struct {
		res installer.Result
		err error
	}
)

// Model is the Bubble Tea model. It is a pointer type so huh's bound values stay valid.
type Model struct {
	cfg  Config
	st   styles
	msg  ui.Messages
	w, h int

	screen screen
	stage  stage
	inv    installer.Inventory
	opts   installer.Options
	need   ui.Need
	cur    int
	scroll int

	memoryAsked, modeDecided, personaAsked bool
	gentleKiroAsked                        bool
	teamAsked, engramAsked                 bool
	teamAsked2                             []string
	form                                   *huh.Form
	formKind                               string
	nameVal, modeVal                       string
	urlVal, tokenEnvVal, storeVal          string
	secretVal, invitesVal                  string
	gitVal                                 bool
	rtVal                                  []string

	personaBlocks []rt.Action
	plan          installer.Plan
	showDetails   bool

	steps      []step
	stream     chan tea.Msg
	cancel     context.CancelFunc
	cancelling bool
	retiring   bool
	retire     *retireAskMsg
	retireHelp bool

	ran       bool
	res       installer.Result
	runErr    error
	cancelled bool
	fatal     string
	quitting  bool
}

// New builds the model.
func New(cfg Config) *Model {
	if cfg.Messages == nil {
		cfg.Messages = ui.Default()
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Minute
	}
	return &Model{cfg: cfg, st: newStyles(!cfg.NoColor), msg: cfg.Messages, opts: cfg.Options, w: 80, h: 24}
}

// finish makes sure no apply is left running when the program ends for any
// reason (a closed terminal, a signal): it asks the pipeline to roll back and
// waits for it, so the process never exits half-applied.
func (m *Model) finish() {
	if m.stream == nil {
		return
	}
	if m.cancel != nil {
		m.cancel()
	}
	for {
		switch x := (<-m.stream).(type) {
		case doneMsg:
			m.cancelling = true
			m.onDone(x)
			return
		case retireAskMsg:
			x.reply <- false
		}
	}
}

// Outcome reports what happened once the program ended.
func (m *Model) Outcome() Outcome {
	return Outcome{Ran: m.ran, Result: m.res, Err: m.runErr, Cancelled: m.cancelled, Inv: m.inv, Source: m.opts.Source}
}

// Init starts detection (read-only).
func (m *Model) Init() tea.Cmd {
	return func() tea.Msg { return detectedMsg{m.cfg.Backend.Detect(context.Background())} }
}

// Update implements tea.Model. After every message it keeps the cursor line in
// view when the screen or the cursor moved (manual scrolling is left alone).
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	prevScreen, prevCur := m.screen, m.cur
	model, cmd := m.update(msg)
	if m.screen != prevScreen {
		m.scroll = 0
	}
	if m.screen != prevScreen || m.cur != prevCur {
		m.ensureFocus()
	}
	return model, cmd
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.ensureFocus()
	case detectedMsg:
		m.inv = msg.inv
		m.screen = scrHome
		return m, nil
	case planMsg:
		return m, m.onPlan(msg)
	case eventMsg:
		m.onEvent(installer.Event(msg))
		return m, waitFor(m.stream)
	case retireAskMsg:
		m.retire, m.screen, m.cur = &msg, scrRetire, 1
		return m, waitFor(m.stream)
	case doneMsg:
		m.onDone(msg)
		return m, nil
	case tea.InterruptMsg:
		return m, m.interrupt()
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, m.interrupt()
		}
		if m.screen == scrForm && m.form != nil {
			return m, m.formKey(msg)
		}
		return m, m.key(msg)
	}
	if m.screen == scrForm && m.form != nil { // blink, window size and huh's own messages
		return m, m.formUpdate(msg)
	}
	return m, nil
}

// interrupt is ctrl+c. Before apply nothing was changed, so it quits. While
// applying it never quits: it asks the pipeline to roll back and waits for it.
func (m *Model) interrupt() tea.Cmd {
	switch m.screen {
	case scrApply:
		if m.retiring {
			m.addStep(ui.SymWarn, m.msg.Format("tui.apply.noretire"))
			return nil
		}
		if !m.cancelling {
			m.cancelling = true
			m.addStep(ui.SymWarn, m.msg.Format("tui.apply.cancelling"))
			if m.cancel != nil {
				m.cancel()
			}
		}
		return nil
	case scrRetire:
		// The question is asked after verification, so cancelling means "keep it":
		// everything GabitoTL did stays, Gentle-AI is not touched.
		m.answerRetire(false)
		m.addStep(ui.SymWarn, m.msg.Format("tui.apply.retire_keep"))
		return nil
	}
	m.quitting = true
	return tea.Quit
}

type menuItem struct{ id, label, desc string }

func (m *Model) items() []menuItem {
	f := m.msg.Format
	switch m.screen {
	case scrHome:
		var it []menuItem
		if m.inv.Other.GentleAI.Found {
			it = append(it, menuItem{id: "migrate", label: f("ask.action.migrate")}, menuItem{id: "install", label: f("ask.action.keep")})
		} else {
			it = append(it, menuItem{id: "install", label: f("tui.action.install")})
		}
		return append(it, menuItem{id: "details", label: f("ask.action.inventory")}, menuItem{id: "exit", label: f("ask.action.exit")})
	case scrMemory:
		return []menuItem{
			{id: "use", label: f("tui.memory.use"), desc: f("tui.memory.use.desc")},
			{id: "team", label: f("tui.memory.team"), desc: f("tui.memory.team.desc") + " " + f("tui.memory.team.limit")},
		}
	case scrTeam:
		var it []menuItem
		if m.inv.Server.Engine != "" {
			it = append(it, menuItem{id: "team-host", label: f("tui.team.host"), desc: f("tui.team.host.desc")})
		}
		return append(it,
			menuItem{id: "team-client", label: f("tui.team.client"), desc: f("tui.team.client.desc")},
			menuItem{id: "team-stay", label: f("tui.team.stay"), desc: f("tui.team.stay.desc")})
	case scrEngram:
		return []menuItem{
			{id: "engram-import", label: f("tui.engram.import"), desc: f("tui.engram.import.desc")},
			{id: "engram-keep", label: f("tui.engram.keep"), desc: f("tui.engram.keep.desc")},
			{id: "engram-empty", label: f("tui.engram.empty"), desc: f("tui.engram.empty.desc")},
		}
	case scrPersona:
		return []menuItem{{id: "persona-do", label: f("tui.persona.do")}, {id: "persona-keep", label: f("tui.persona.keep")}}
	case scrGentleKiro:
		return []menuItem{{id: "kiro-keep", label: f("tui.gentle-kiro.keep")}, {id: "kiro-do", label: f("tui.gentle-kiro.do")}}
	case scrPlan:
		var it []menuItem
		switch {
		case !m.plan.Mutates():
		case m.opts.DryRun:
			it = append(it, menuItem{id: "apply", label: f("tui.plan.dry")})
		default:
			it = append(it, menuItem{id: "apply", label: f("tui.plan.apply")})
		}
		toggle := f("tui.plan.details")
		if m.showDetails {
			toggle = f("tui.plan.hide")
		}
		return append(it, menuItem{id: "toggle", label: toggle}, menuItem{id: "exit", label: f("tui.plan.exit")})
	case scrRetire:
		return []menuItem{{id: "retire-do", label: f("ask.retire.do")}, {id: "retire-keep", label: f("ask.retire.keep")}, {id: "retire-review", label: f("ask.retire.review")}}
	}
	return nil
}

func (m *Model) key(k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	items := m.items()
	switch s {
	case "up", "k":
		if len(items) > 0 {
			m.cur = max(0, m.cur-1)
			m.ensureFocus()
		} else {
			m.scroll = max(0, m.scroll-1)
		}
		return nil
	case "down", "j":
		if len(items) > 0 {
			m.cur = min(len(items)-1, m.cur+1)
			m.ensureFocus()
		} else {
			m.scroll++
			m.clampScroll()
		}
		return nil
	case "pgup":
		m.scroll = max(0, m.scroll-m.bodyRows())
		return nil
	case "pgdown":
		m.scroll += m.bodyRows()
		m.clampScroll()
		return nil
	}
	switch m.screen {
	case scrApply, scrBuilding, scrDetect:
		if s == "q" && m.screen != scrApply {
			return m.quit()
		}
		return nil
	case scrRetire:
		if s == "enter" && m.cur < len(items) {
			return m.chooseRetire(items[m.cur].id)
		}
		return nil
	case scrResult:
		if s == "enter" || s == "q" || s == "esc" {
			return m.quit()
		}
		return nil
	case scrDetails:
		if s == "enter" || s == "esc" || s == "q" {
			m.screen, m.cur, m.scroll = scrHome, 0, 0
		}
		return nil
	}
	switch s {
	case "q":
		return m.quit()
	case "esc":
		if m.screen != scrHome {
			m.reset()
		}
		return nil
	case "enter":
		if m.cur < len(items) {
			return m.choose(items[m.cur].id)
		}
	}
	return nil
}

func (m *Model) quit() tea.Cmd {
	m.quitting = true
	return tea.Quit
}

// reset goes back to the home menu and forgets the answers of this attempt.
func (m *Model) reset() {
	m.screen, m.stage, m.cur, m.scroll = scrHome, stMemory, 0, 0
	m.opts = m.cfg.Options
	m.memoryAsked, m.modeDecided, m.personaAsked, m.gentleKiroAsked, m.showDetails = false, false, false, false, false
	m.teamAsked, m.engramAsked, m.teamAsked2 = false, false, nil
	m.form, m.personaBlocks = nil, nil
}

func (m *Model) choose(id string) tea.Cmd {
	switch id {
	case "exit":
		return m.quit()
	case "details":
		m.screen, m.scroll = scrDetails, 0
	case "migrate", "install":
		m.opts = m.cfg.Options
		m.opts.Source = installer.SourceInstall
		if id == "migrate" {
			m.opts.Source = installer.SourceGentleAI
		}
		if m.cfg.Need != nil {
			m.need = m.cfg.Need(m.inv, m.opts)
		}
		if m.need.Teammate && m.opts.MemoryRole == "" {
			// The committed memory.md already names the server: no role or url question.
			m.opts.Mode, m.opts.Memory, m.opts.MemoryRole, m.opts.MemoryURL = gabitotl.ModeTeam, gabitotl.MemoryMemoryMCP, gabitotl.RoleClient, m.need.SharedURL
			m.teamAsked = true
		}
		return m.advance()
	case "use", "team":
		m.memoryAsked, m.modeDecided = true, true
		if id == "team" {
			m.opts.Mode, m.opts.Memory = gabitotl.ModeTeam, gabitotl.MemoryMemoryMCP
		} else {
			m.opts.Mode = gabitotl.ModeSolo
		}
		return m.advance()
	case "team-host", "team-client", "team-stay":
		return m.chooseTeam(id)
	case "engram-import", "engram-keep", "engram-empty":
		m.engramAsked = true
		m.opts.ImportEngram = id == "engram-import"
		return m.advance()
	case "persona-do", "persona-keep":
		m.personaAsked = true
		m.opts.MigratePersona = id == "persona-do"
		return m.planCmd(m.opts, false)
	case "kiro-do", "kiro-keep":
		m.gentleKiroAsked = true
		if id == "kiro-keep" {
			m.screen = scrPlan // m.plan already is the plan that keeps both
			return nil
		}
		m.opts.RetireGentleKiro = true
		return m.planCmd(m.opts, false) // replan: the plan then lists exactly what moves
	case "toggle":
		m.showDetails = !m.showDetails
	case "apply":
		return m.startApply()
	}
	return nil
}

// advance moves to the next stage that applies and returns its command.
func (m *Model) advance() tea.Cmd {
	for {
		st := m.stage
		m.stage++
		m.cur, m.scroll = 0, 0
		switch st {
		case stMemory:
			if m.inv.Memory.Present() && m.opts.Memory == "" && !m.memoryAsked {
				m.screen = scrMemory
				return nil
			}
		case stName:
			if m.need.Name {
				return m.startForm("name")
			}
		case stMode:
			if m.need.Mode && !m.modeDecided {
				return m.startForm("mode")
			}
		case stTeam:
			if m.wantsTeam() && !m.teamAsked && m.opts.MemoryRole == "" {
				m.stage-- // come back here with the answer
				m.screen = scrTeam
				return nil
			}
			if next := m.nextTeamForm(); next != "" {
				m.stage-- // come back to this stage until every form is answered
				return m.startForm(next)
			}
		case stEngram:
			if m.wantsTeam() && m.opts.MemoryRole == gabitotl.RoleHost && m.inv.Memory.Present() && m.inv.Memory.ProjectMatch != "" &&
				m.inv.Server.CanImport && !m.engramAsked && !m.opts.ImportEngram {
				m.screen = scrEngram
				return nil
			}
		case stGit:
			if m.need.Git {
				return m.startForm("git")
			}
		case stRuntimes:
			if len(m.inv.Runtimes) > 0 && len(m.cfg.Options.Runtimes) == 0 {
				return m.startForm("runtimes")
			}
		default:
			return m.planCmd(m.opts, false)
		}
	}
}

func (m *Model) planCmd(opts installer.Options, preview bool) tea.Cmd {
	m.screen = scrBuilding
	b, inv := m.cfg.Backend, m.inv
	return func() tea.Msg {
		p, err := b.BuildPlan(context.Background(), inv, opts)
		return planMsg{plan: p, err: err, preview: preview}
	}
}

func (m *Model) onPlan(p planMsg) tea.Cmd {
	m.cur, m.scroll = 0, 0
	switch {
	case p.err != nil:
		m.fatal = p.err.Error()
		m.screen = scrResult
	case p.preview:
		m.personaBlocks = p.plan.RetireActions()
		m.screen = scrPersona
		if len(m.personaBlocks) == 0 {
			m.personaAsked = true
			m.screen = scrPlan
			if m.asksGentleKiro(m.plan) {
				m.screen = scrGentleKiro
			}
		}
	case !m.personaAsked && !m.opts.MigratePersona && p.plan.HasNote(installer.NotePersonaConflict):
		po := m.opts
		po.MigratePersona = true
		m.plan = p.plan
		return m.planCmd(po, true)
	case m.asksGentleKiro(p.plan):
		m.plan, m.screen = p.plan, scrGentleKiro
	default:
		m.plan, m.screen = p.plan, scrPlan
	}
	return nil
}

// asksGentleKiro reports whether the plan found Gentle AI's files in Kiro that
// the user has neither retired (flag or answer) nor been asked about yet.
func (m *Model) asksGentleKiro(p installer.Plan) bool {
	return !m.gentleKiroAsked && !m.opts.RetireGentleKiro && p.GentleKiro != nil && !p.GentleKiro.Empty()
}

// wantsTeam reports whether the choices so far lead to team memory.
func (m *Model) wantsTeam() bool {
	return m.opts.Memory == gabitotl.MemoryMemoryMCP || m.opts.Mode == gabitotl.ModeTeam
}

// chooseTeam applies the role the user picked on the team screen.
func (m *Model) chooseTeam(id string) tea.Cmd {
	m.teamAsked = true
	switch id {
	case "team-stay":
		m.opts.Mode, m.opts.Memory, m.opts.MemoryRole = gabitotl.ModeSolo, "", ""
	case "team-host":
		m.opts.Mode, m.opts.Memory, m.opts.MemoryRole = gabitotl.ModeTeam, gabitotl.MemoryMemoryMCP, gabitotl.RoleHost
	case "team-client":
		m.opts.Mode, m.opts.Memory, m.opts.MemoryRole = gabitotl.ModeTeam, gabitotl.MemoryMemoryMCP, gabitotl.RoleClient
	}
	return m.advance()
}

// nextTeamForm returns the next question the chosen role still needs ("" when
// done). Answers given through flags are never asked again.
func (m *Model) nextTeamForm() string {
	if !m.wantsTeam() || m.opts.MemoryRole == "" {
		return ""
	}
	asked := func(k string) bool {
		for _, a := range m.teamAsked2 {
			if a == k {
				return true
			}
		}
		return false
	}
	switch m.opts.MemoryRole {
	case gabitotl.RoleHost:
		if !asked("hosturl") && m.opts.MemoryURL == "" {
			return "hosturl" // the address teammates use; may stay empty
		}
		if !asked("invites") && len(m.cfg.Options.Invites) == 0 {
			return "invites"
		}
	case gabitotl.RoleClient:
		switch {
		case !asked("url") && m.opts.MemoryURL == "":
			return "url"
		case !asked("tokenenv") && m.opts.MemoryTokenEnv == "":
			return "tokenenv"
		case !asked("storetoken") && !m.opts.StoreToken:
			return "storetoken"
		case m.opts.StoreToken && !asked("secret") && m.storeVal == "yes":
			return "secret"
		}
	}
	return ""
}
