package tui

import (
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
)

// NoColor reports whether the environment asks for no colors (https://no-color.org).
func NoColor(getenv func(string) string) bool { return getenv("NO_COLOR") != "" }

// Run shows the installer on in/out until the user finishes or quits, and
// reports what happened. Nothing is written before the plan is applied, and the
// apply phase can only end by completing or by the pipeline's own rollback.
func Run(cfg Config, in io.Reader, out io.Writer) (Outcome, error) {
	m := New(cfg)
	opts := []tea.ProgramOption{tea.WithInput(in), tea.WithOutput(out)}
	if cfg.NoColor {
		opts = append(opts, tea.WithEnvironment(append(os.Environ(), "NO_COLOR=1")))
	}
	_, err := tea.NewProgram(m, opts...).Run()
	m.finish()
	return m.Outcome(), err
}
