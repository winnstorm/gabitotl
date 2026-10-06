package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
)

// step is one progress line of the apply screen. The symbol carries the state.
type step struct{ sym, text string }

func (m *Model) addStep(sym, text string) { m.steps = append(m.steps, step{sym, text}) }

// settle marks the line in progress as done.
func (m *Model) settle() {
	if n := len(m.steps); n > 0 && m.steps[n-1].sym == ui.SymTodo {
		m.steps[n-1].sym = ui.SymOK
	}
}

// startApply runs the installer pipeline in a goroutine and streams its events
// back as messages. The pipeline owns order, snapshot and rollback; the model
// only shows them.
func (m *Model) startApply() tea.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), m.cfg.Timeout)
	m.cancel, m.screen, m.steps, m.cancelling, m.retiring = cancel, scrApply, nil, false, false
	ch := make(chan tea.Msg, 256)
	m.stream = ch
	b, inv, plan := m.cfg.Backend, m.inv, m.plan
	obs := func(e installer.Event) { ch <- eventMsg(e) }
	hooks := installer.Hooks{ConfirmRetire: func(pv installer.RetirePreview) bool {
		reply := make(chan bool, 1)
		ch <- retireAskMsg{preview: pv, reply: reply}
		select {
		case ok := <-reply:
			return ok
		case <-ctx.Done():
			return false
		}
	}}
	go func() {
		defer cancel()
		res, err := b.Run(ctx, inv, plan, obs, hooks)
		ch <- doneMsg{res: res, err: err}
	}()
	return waitFor(ch)
}

func waitFor(ch <-chan tea.Msg) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg { return <-ch }
}

// onEvent turns pipeline events into progress lines.
func (m *Model) onEvent(e installer.Event) {
	f := m.msg.Format
	switch {
	case e.Phase == installer.PhaseReport && e.Status == "error":
		m.settle()
		m.addStep(ui.SymError, f("tui.apply.rollback"))
	case e.Phase == installer.PhaseReport && e.Step == "rolled back":
		n, _, _ := strings.Cut(e.Detail, " ")
		m.addStep(ui.SymOK, f("tui.apply.rolled", "count", n))
	case e.Status == "start":
		if text, ok := m.msg["phase."+string(e.Phase)]; ok {
			m.settle()
			m.addStep(ui.SymTodo, text+"…")
			m.retiring = e.Phase == installer.PhaseRetire
		}
	case e.Status == "ok" && strings.HasPrefix(e.Step, "configure "):
		m.addStep(ui.SymOK, f("tui.step.configure", "name", m.runtimeLabel(strings.TrimPrefix(e.Step, "configure "))))
	case e.Status == "warn":
		m.addStep(ui.SymWarn, strings.TrimSpace(e.Step+" "+e.Detail))
	}
}

func (m *Model) runtimeLabel(name string) string {
	if r, ok := m.inv.Runtime(name); ok {
		return r.Label
	}
	return name
}

func (m *Model) onDone(d doneMsg) {
	m.settle()
	m.ran, m.res, m.runErr = true, d.res, d.err
	m.cancelled = m.cancelling && errors.Is(d.err, context.Canceled)
	m.stream, m.retire, m.retiring = nil, nil, false
	m.screen, m.scroll = scrResult, 0
}

// chooseRetire answers the "retire Gentle-AI?" question the pipeline asks after
// verification.
func (m *Model) chooseRetire(id string) tea.Cmd {
	switch id {
	case "retire-review":
		m.retireHelp = !m.retireHelp
	case "retire-do":
		m.answerRetire(true)
	default:
		m.answerRetire(false)
	}
	return nil
}

func (m *Model) answerRetire(ok bool) {
	if m.retire != nil {
		m.retire.reply <- ok
		m.retire = nil
	}
	m.screen, m.retireHelp = scrApply, false
}

// resultLines is the final screen body: the same report the plain flow prints.
func (m *Model) resultLines() []string {
	var b strings.Builder
	switch {
	case m.fatal != "":
		fmt.Fprintf(&b, "%s %s\n", ui.SymError, m.msg.Format("result.failed", "detail", m.fatal))
	case m.cancelled:
		fmt.Fprintf(&b, "%s %s\n", ui.SymOK, m.msg.Format("tui.apply.cancelled"))
	default:
		if m.cancelling && m.runErr == nil {
			fmt.Fprintf(&b, "%s %s\n\n", ui.SymWarn, m.msg.Format("tui.apply.toolate"))
		}
		ui.RenderResult(&b, m.msg, m.res, m.inv, m.cfg.Paths)
		if m.runErr != nil {
			var partial *installer.PartialError
			key := "result.failed"
			if errors.As(m.runErr, &partial) {
				key = "result.partial"
			}
			fmt.Fprintf(&b, "%s %s\n", ui.SymError, m.msg.Format(key, "detail", m.runErr.Error()))
		}
	}
	return strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
}
