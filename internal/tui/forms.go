package tui

import (
	"errors"
	"slices"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// startForm opens one huh form with a single question: onboarding asks one
// thing at a time and only what cannot be detected.
func (m *Model) startForm(kind string) tea.Cmd {
	f := m.msg.Format
	var field huh.Field
	switch kind {
	case "name":
		m.nameVal = m.cfg.SuggestedName
		field = huh.NewInput().Title(f("ask.name")).Value(&m.nameVal)
	case "mode":
		m.modeVal = gabitotl.ModeSolo
		field = huh.NewSelect[string]().Title(f("ask.mode")).Description(f("tui.mode.limit")).
			Options(huh.NewOption(f("ask.mode.solo"), gabitotl.ModeSolo), huh.NewOption(f("ask.mode.team"), gabitotl.ModeTeam)).
			Value(&m.modeVal)
	case "git":
		m.gitVal = false
		field = huh.NewSelect[bool]().Title(f("ask.git")).
			Options(huh.NewOption(f("ask.git.yes"), true), huh.NewOption(f("ask.git.no"), false)).
			Value(&m.gitVal)
	case "runtimes":
		m.rtVal = nil
		var opts []huh.Option[string]
		for _, r := range m.inv.Runtimes {
			label := r.Label
			if !r.Usable() {
				label = f("tui.runtimes.missing", "name", r.Label)
			}
			opts = append(opts, huh.NewOption(label, r.Name).Selected(r.Usable()))
		}
		field = huh.NewMultiSelect[string]().Title(f("tui.runtimes.title")).Description(f("tui.runtimes.desc")).
			Options(opts...).Value(&m.rtVal).
			Validate(func(v []string) error {
				if len(v) == 0 {
					return errors.New(f("tui.runtimes.min"))
				}
				return nil
			})
	}
	theme := huh.ThemeFunc(huh.ThemeCharm)
	if m.cfg.NoColor {
		theme = huh.ThemeFunc(huh.ThemeBase)
	}
	m.form = huh.NewForm(huh.NewGroup(field)).WithTheme(theme).WithShowHelp(false).WithWidth(max(20, m.w-4))
	m.formKind, m.screen, m.scroll = kind, scrForm, 0
	return m.form.Init()
}

func (m *Model) formKey(k tea.KeyPressMsg) tea.Cmd {
	if k.String() == "esc" {
		m.reset()
		return nil
	}
	return m.formUpdate(k)
}

func (m *Model) formUpdate(msg tea.Msg) tea.Cmd {
	model, cmd := m.form.Update(msg)
	m.form = model.(*huh.Form)
	if m.form.State == huh.StateCompleted {
		return tea.Batch(cmd, m.formDone())
	}
	return cmd
}

// formDone copies the answer into the installer options and moves on.
func (m *Model) formDone() tea.Cmd {
	switch m.formKind {
	case "name":
		if m.nameVal != "" {
			m.opts.UserName = m.nameVal
		}
	case "mode":
		m.opts.Mode = m.modeVal
		m.modeDecided = true
	case "git":
		m.opts.AutomateGit = m.gitVal
	case "runtimes":
		m.opts.Runtimes = slices.Clone(m.rtVal)
	}
	m.form = nil
	return m.advance()
}
