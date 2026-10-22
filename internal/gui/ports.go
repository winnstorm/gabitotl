package gui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/winnstorm/gabitotl/internal/codeindex"
	"github.com/winnstorm/gabitotl/internal/desktop"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
)

// CodeIndex detects and sets up the optional code index tools.
type CodeIndex interface {
	Backends(root string) []IndexBackend
	// Setup indexes root with the named tool through run (no shell).
	Setup(ctx context.Context, run installer.Runner, root, tool string) error
}

// osCodeIndex drives the real tools found on PATH.
type osCodeIndex struct{}

func (osCodeIndex) Backends(root string) []IndexBackend {
	var out []IndexBackend
	for _, b := range codeindex.All() {
		st := b.Detect(root)
		out = append(out, IndexBackend{Name: b.Name(), Installed: st.Installed, Indexed: st.Indexed, Hint: b.InstallHint()})
	}
	return out
}

func (osCodeIndex) Setup(ctx context.Context, run installer.Runner, root, tool string) error {
	b, ok := codeindex.ByName(tool)
	if !ok {
		return fmt.Errorf("unknown code index tool %q (want codegraph or code-review-graph)", tool)
	}
	if !b.Detect(root).Installed {
		return fmt.Errorf("%s is not installed; %s", b.Name(), b.InstallHint())
	}
	argv := b.SetupCommand(root)
	if _, errb, err := run.Run(ctx, root, argv[0], argv[1:]...); err != nil {
		return fmt.Errorf("%s failed: %w %s", argv[0], err, strings.TrimSpace(string(errb)))
	}
	return nil
}

func (s *Service) codeIndex() CodeIndex {
	if s.d.CodeIndex != nil {
		return s.d.CodeIndex
	}
	return osCodeIndex{}
}

// desktopOptions turns the Windows screen into the port's options.
func (s *Service) desktopOptions(w WindowsAnswers) desktop.Options {
	return desktop.Options{
		InstallDir: w.InstallDir, Version: s.d.Version,
		AddToPath: w.AddToPath, StartMenu: w.StartMenu, DesktopShortcut: w.DesktopShortcut,
	}
}

// desktopSteps plans the Windows integration. supported is false on an OS that
// has none: that is a result, not a failure, so a Linux or macOS install still
// completes. A real problem (an unsafe directory) stays an error.
func (s *Service) desktopSteps(o desktop.Options) (steps []desktop.Step, supported bool, err error) {
	if s.d.Desktop == nil {
		return nil, false, nil
	}
	steps, err = s.d.Desktop.Plan(o)
	switch {
	case isUnsupported(err):
		return nil, false, nil
	case err != nil:
		return nil, true, err
	}
	return steps, true, nil
}

// DesktopStatus reports what the Windows integration currently has in place.
func (s *Service) DesktopStatus() DesktopStatus {
	if s.d.Desktop == nil {
		return DesktopStatus{}
	}
	st := s.d.Desktop.Status(desktop.Options{})
	if st.InstallDir == "" {
		return DesktopStatus{}
	}
	return DesktopStatus{
		Supported: true, InstallDir: st.InstallDir, Installed: st.Installed(), Version: st.Version,
		OnPath: st.OnPath, StartMenu: st.StartMenu, DesktopLink: st.DesktopLink,
	}
}

// applyDesktop runs the Windows integration, reporting each step.
func (s *Service) applyDesktop(o desktop.Options, p Progress, from, to int) StepResult {
	m := ui.Active("")
	steps, supported, err := s.desktopSteps(o)
	switch {
	case err != nil:
		return StepResult{ID: "desktop", Status: StatusError, Message: err.Error()}
	case !supported:
		return StepResult{ID: "desktop", Status: StatusUnsupported}
	}
	n := 0
	err = s.d.Desktop.Apply(o, func(st desktop.Step) {
		p.emit("desktop."+st.ID, "running", desktopTitle(m, st), from+(to-from)*n/max(len(steps), 1))
		n++
	})
	if err != nil {
		if errors.Is(err, desktop.ErrUnsupported) {
			return StepResult{ID: "desktop", Status: StatusUnsupported}
		}
		return StepResult{ID: "desktop", Status: StatusError, Message: err.Error()}
	}
	if cli := s.d.Desktop.CLITarget(o); cli != "" {
		s.setCLI(cli)
	}
	return StepResult{ID: "desktop", Status: StatusOK}
}

// desktopTitle is the localized title of a desktop step, or its English title
// when the catalogue has no entry for it.
func desktopTitle(m ui.Messages, st desktop.Step) string {
	if st.ID == desktop.StepClose {
		if st.Count == 1 {
			return m.Format("gui.step.close-copies.one")
		}
		return m.Format("gui.step.close-copies.many", "count", strconv.Itoa(st.Count))
	}
	key := "gui.desktop.step." + st.ID
	if _, ok := m[key]; !ok {
		return st.Title
	}
	return m.Format(key, "dir", st.Dir)
}

// desktopPlanSteps maps desktop steps to plan steps. The "close running copies"
// step gets its localized title and a note that unsaved work in those copies is
// lost; the other titles pass through.
func (s *Service) desktopPlanSteps(m ui.Messages, o desktop.Options, ds []desktop.Step) ([]PlanStep, []Note) {
	var steps []PlanStep
	var notes []Note
	for _, d := range ds {
		st := PlanStep{ID: "desktop." + d.ID, Title: desktopTitle(m, d)}
		if d.ID == desktop.StepClose && s.d.Desktop != nil {
			n := len(s.d.Desktop.RunningCopies(o))
			key := "gui.step.close-copies.many"
			if n == 1 {
				key = "gui.step.close-copies.one"
			}
			st.Title = m.Format(key, "count", strconv.Itoa(n))
			notes = append(notes, Note{Code: "desktop.close", Text: m.Format("gui.close-copies.warn", "count", strconv.Itoa(n))})
		}
		steps = append(steps, st)
	}
	return steps, notes
}
