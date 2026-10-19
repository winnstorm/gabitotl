package gui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/winnstorm/gabitotl/internal/codeindex"
	"github.com/winnstorm/gabitotl/internal/desktop"
	"github.com/winnstorm/gabitotl/internal/installer"
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
	steps, supported, err := s.desktopSteps(o)
	switch {
	case err != nil:
		return StepResult{ID: "desktop", Status: StatusError, Message: err.Error()}
	case !supported:
		return StepResult{ID: "desktop", Status: StatusUnsupported}
	}
	n := 0
	err = s.d.Desktop.Apply(o, func(st desktop.Step) {
		p.emit("desktop."+st.ID, "running", st.Title, from+(to-from)*n/max(len(steps), 1))
		n++
	})
	if err != nil {
		if errors.Is(err, desktop.ErrUnsupported) {
			return StepResult{ID: "desktop", Status: StatusUnsupported}
		}
		return StepResult{ID: "desktop", Status: StatusError, Message: err.Error()}
	}
	return StepResult{ID: "desktop", Status: StatusOK}
}
