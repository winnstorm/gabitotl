package tui

import (
	"errors"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
)

// deliveryPrefix marks the form kinds of the delivery questions ("vcs:<step>").
const deliveryPrefix = "vcs:"

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
		question, solo, team := "ask.mode", "ask.mode.solo", "ask.mode.team"
		if m.need.Current != "" { // the project already has a provider: offer to change it
			question, solo, team = "ask.change", "ask.change.no", "ask.change.yes"
		}
		field = huh.NewSelect[string]().Title(f(question)).Description(f("tui.mode.limit")).
			Options(huh.NewOption(f(solo), gabitotl.ModeSolo), huh.NewOption(f(team), gabitotl.ModeTeam)).
			Value(&m.modeVal)
	case "lang":
		m.langVal = "es"
		field = huh.NewSelect[string]().Title(f("ask.lang")).
			Options(huh.NewOption(f("ask.lang.es"), "es"), huh.NewOption(f("ask.lang.en"), "en")).Value(&m.langVal)
	case "invites":
		m.invitesVal = ""
		field = huh.NewInput().Title(f("tui.team.invites")).Description(f("tui.team.invites.desc")).Value(&m.invitesVal).
			Validate(func(v string) error {
				for _, id := range splitIDs(v) {
					if !installer.ValidTeamUser(id) {
						return errors.New(id + " no es un id válido")
					}
				}
				return nil
			})
	case "url":
		m.urlVal = ""
		field = huh.NewInput().Title(f("tui.team.url")).Description(f("tui.team.url.desc")).Value(&m.urlVal).
			Validate(func(v string) error {
				if err := installer.ValidateTeamURL(strings.TrimSpace(v)); err != nil {
					return errors.New(f("tui.team.badurl", "detail", err.Error()))
				}
				return nil
			})
	case "hosturl":
		m.urlVal = ""
		field = huh.NewInput().Title(f("tui.team.hosturl")).Description(f("tui.team.hosturl.desc")).Value(&m.urlVal).
			Validate(func(v string) error {
				if v = strings.TrimSpace(v); v == "" {
					return nil // optional: nothing is shared until the host has an address
				}
				if err := installer.ValidateTeamHostURL(v); err != nil {
					return errors.New(f("tui.team.badurl", "detail", err.Error()))
				}
				return nil
			})
	case "tokenenv":
		m.tokenEnvVal = installer.DefaultTokenEnv
		field = huh.NewInput().Title(f("tui.team.tokenenv")).Description(f("tui.team.tokenenv.desc")).Value(&m.tokenEnvVal).
			Validate(func(v string) error {
				if !installer.ValidTokenEnv(strings.TrimSpace(v)) {
					return errors.New(f("tui.team.badenv"))
				}
				return nil
			})
	case "storetoken":
		m.storeVal = "no"
		field = huh.NewSelect[string]().Title(f("tui.team.store")).
			Options(huh.NewOption(f("tui.team.store.yes"), "yes"), huh.NewOption(f("tui.team.store.no"), "no")).Value(&m.storeVal)
	case "secret":
		m.secretVal = ""
		field = huh.NewInput().Title(f("tui.team.secret")).Description(f("tui.team.secret.desc")).EchoMode(huh.EchoModePassword).Value(&m.secretVal).
			Validate(func(v string) error {
				if strings.TrimSpace(v) == "" || strings.ContainsAny(v, "\r\n") {
					return errors.New(f("tui.team.badenv"))
				}
				return nil
			})
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
	default:
		if step, ok := strings.CutPrefix(kind, deliveryPrefix); ok {
			title, opts, def := m.msg.DeliveryQuestion(ui.DeliveryStep(step), m.need)
			options := make([]huh.Option[string], len(opts))
			for i, o := range opts {
				options[i] = huh.NewOption(o.Label, o.Value)
			}
			m.vcsVal = opts[def].Value
			field = huh.NewSelect[string]().Title(title).Options(options...).Value(&m.vcsVal)
		}
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
	case "lang":
		m.opts.PersonaLang = m.langVal
	case "runtimes":
		m.opts.Runtimes = slices.Clone(m.rtVal)
	case "invites":
		m.opts.Invites = splitIDs(m.invitesVal)
		m.teamAsked2 = append(m.teamAsked2, "invites")
	case "url":
		m.opts.MemoryURL = strings.TrimSpace(m.urlVal)
		m.teamAsked2 = append(m.teamAsked2, "url")
	case "hosturl":
		m.opts.MemoryURL = strings.TrimSpace(m.urlVal)
		m.teamAsked2 = append(m.teamAsked2, "hosturl")
	case "tokenenv":
		m.opts.MemoryTokenEnv = strings.TrimSpace(m.tokenEnvVal)
		m.teamAsked2 = append(m.teamAsked2, "tokenenv")
	case "storetoken":
		m.opts.StoreToken = m.storeVal == "yes"
		m.teamAsked2 = append(m.teamAsked2, "storetoken")
	case "secret":
		m.opts.TokenSecret = m.secretVal // held in memory for the apply only
		m.secretVal = ""
		m.teamAsked2 = append(m.teamAsked2, "secret")
	default:
		if step, ok := strings.CutPrefix(m.formKind, deliveryPrefix); ok {
			ui.ApplyDeliveryAnswer(&m.opts.Delivery, ui.DeliveryStep(step), m.vcsVal)
			m.deliveryDone = append(m.deliveryDone, ui.DeliveryStep(step))
		}
	}
	m.form = nil
	return m.advance()
}

func splitIDs(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
