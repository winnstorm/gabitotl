package gui

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/winnstorm/gabitotl/internal/desktop"
	"github.com/winnstorm/gabitotl/internal/installer"
)

// Fix action ids. Each maps to an existing code path.
const (
	FixRuntimeRepair  = "runtime.repair"      // drift: re-apply the runtime
	FixRuntimeConnect = "runtime.connect"     // todo: first apply of a detected runtime
	FixVCSLogin       = "vcs.login-check"     // check the Git platform login
	FixGuardHistory   = "guard.scan-history"  // audit existing commits
	FixGuardHooks     = "guard.install-hooks" // install the authorship Git hooks
	FixCleanupLegacy  = "cleanup.legacy"      // delete expired legacy snapshots
	FixDesktop        = "desktop.reapply"     // repair the Windows integration
	FixEngramInstall  = "engram.install"      // install Engram (pinned, checksum verified)
)

// FixAction is the button next to a doctor line.
type FixAction struct {
	ID      string            `json:"id"`
	Label   string            `json:"label"`
	Args    map[string]string `json:"args,omitempty"`
	Mutates bool              `json:"mutates"` // true: plan it first
}

// DoctorLine is one finding. Message is already in the active language.
type DoctorLine struct {
	Status  string     `json:"status"` // ok | warn | todo | error
	Code    string     `json:"code"`
	Message string     `json:"message"`
	Detail  string     `json:"detail,omitempty"`
	Fix     *FixAction `json:"fix,omitempty"`
}

// DoctorSection groups lines.
type DoctorSection struct {
	ID    string       `json:"id"`
	Title string       `json:"title"`
	Lines []DoctorLine `json:"lines"`
}

// DoctorReport is the Manager's health screen.
type DoctorReport struct {
	Title    string          `json:"title"`
	Sections []DoctorSection `json:"sections"`
	Errors   int             `json:"errors"`
	Summary  string          `json:"summary"`
}

// Doctor runs the read-only doctor and attaches the fix actions. On Windows it
// adds a "desktop" section for the app's own integration.
func (s *Service) Doctor(ctx context.Context, root string) (DoctorReport, error) {
	env, err := s.env(root)
	if err != nil {
		return DoctorReport{}, err
	}
	m := msgs(env, "")
	rep := installer.Doctor(ctx, env) // read-only
	byLabel := map[string]string{}
	for _, a := range env.Runtimes.All() {
		byLabel[a.Label()] = a.Name()
	}
	out := DoctorReport{Title: m.Format("doctor.title"), Sections: []DoctorSection{}, Errors: rep.Errors()}
	for _, sec := range rep.Sections {
		if len(sec.Lines) == 0 {
			continue
		}
		ds := DoctorSection{ID: sec.Name, Title: m.Format("doctor.section." + sec.Name), Lines: []DoctorLine{}}
		for _, l := range sec.Lines {
			ds.Lines = append(ds.Lines, DoctorLine{
				Status: l.Status, Code: l.Code, Message: m.Format(l.Code, "detail", l.Detail), Detail: l.Detail,
				Fix: fixFor(m, l, byLabel),
			})
		}
		out.Sections = append(out.Sections, ds)
	}
	if d := s.desktopSection(m); d != nil {
		out.Sections = append(out.Sections, *d)
	}
	if out.Errors > 0 {
		out.Summary = m.Format("doctor.summary.errors", "count", strconv.Itoa(out.Errors))
	} else {
		out.Summary = m.Format("doctor.summary.ok")
	}
	return out, nil
}

// runtimeOf finds the runtime a doctor line names by its label prefix.
func runtimeOf(detail string, byLabel map[string]string) string {
	best := ""
	for label := range byLabel {
		if (detail == label || strings.HasPrefix(detail, label+" ") || strings.HasPrefix(detail, label+":")) && len(label) > len(best) {
			best = label
		}
	}
	return byLabel[best]
}

// fixFor maps a doctor line to its fix action, or nil when nothing can be done
// from the Manager.
func fixFor(m map[string]string, l installer.Line, byLabel map[string]string) *FixAction {
	label := func(key string) string { return m[key] }
	switch l.Code {
	case installer.DocRuntimeDrift:
		if n := runtimeOf(l.Detail, byLabel); n != "" {
			return &FixAction{ID: FixRuntimeRepair, Label: label("fix.runtime.repair"), Args: map[string]string{"runtime": n}, Mutates: true}
		}
	case installer.DocRuntimeTodo, installer.DocGuardRuntimeTodo:
		if n := runtimeOf(l.Detail, byLabel); n != "" {
			return &FixAction{ID: FixRuntimeConnect, Label: label("fix.runtime.connect"), Args: map[string]string{"runtime": n}, Mutates: true}
		}
	case installer.DocGitAuthTodo, installer.DocGitAuthTool:
		return &FixAction{ID: FixVCSLogin, Label: label("fix.vcs.login-check")}
	case installer.DocGuardHistory:
		return &FixAction{ID: FixGuardHistory, Label: label("fix.guard.scan-history")}
	case installer.DocGuardHooksMissing:
		return &FixAction{ID: FixGuardHooks, Label: label("fix.guard.install-hooks"), Mutates: true}
	case installer.DocMemEngramMissing, installer.DocMemEngramNoBin:
		return &FixAction{ID: FixEngramInstall, Label: label("fix.engram.install"), Mutates: true}
	case installer.DocLegacySnapshots:
		return &FixAction{ID: FixCleanupLegacy, Label: label("fix.cleanup.legacy"), Mutates: true}
	}
	return nil
}

// Doctor line codes of the desktop section (the wording is in the catalogue).
const (
	docDesktopOK      = "doctor.desktop.ok"
	docDesktopPartial = "doctor.desktop.partial"
	docDesktopNone    = "doctor.desktop.none"
)

// desktopSection reports the Windows integration; nil where there is none.
func (s *Service) desktopSection(m map[string]string) *DoctorSection {
	st := s.DesktopStatus()
	if !st.Supported {
		return nil
	}
	fix := &FixAction{ID: FixDesktop, Label: m["fix.desktop.reapply"], Mutates: true}
	line := DoctorLine{Status: "ok", Code: docDesktopOK, Detail: st.InstallDir}
	switch {
	case !st.Installed:
		line.Status, line.Code, line.Fix = "todo", docDesktopNone, fix
	case !st.OnPath || !st.StartMenu:
		line.Status, line.Code, line.Fix = "warn", docDesktopPartial, fix
	}
	line.Message = strings.ReplaceAll(m[line.Code], "{detail}", st.InstallDir)
	return &DoctorSection{ID: "desktop", Title: m["doctor.section.desktop"], Lines: []DoctorLine{line}}
}

// FixRequest is a fix action chosen in the doctor.
type FixRequest struct {
	Root string            `json:"root"`
	ID   string            `json:"id"`
	Args map[string]string `json:"args"`
}

// FixResult is what a fix did.
type FixResult struct {
	ID      string             `json:"id"`
	OK      bool               `json:"ok"`
	Message string             `json:"message,omitempty"`
	SignIn  *SignIn            `json:"signIn,omitempty"`
	Guard   *GuardScan         `json:"guard,omitempty"`
	Runtime *IntegrationResult `json:"runtime,omitempty"`
	Removed []string           `json:"removed,omitempty"`
}

var errUnknownFix = errors.New("unknown fix action")

// PlanFix shows what a fix would do. Read-only fixes (the login check and the
// history audit) return a plan that changes nothing.
func (s *Service) PlanFix(ctx context.Context, req FixRequest) (Plan, error) {
	switch req.ID {
	case FixRuntimeRepair, FixRuntimeConnect:
		return s.PlanIntegration(ctx, IntegrationRequest{Root: req.Root, Runtime: req.Args["runtime"], Op: OpApply})
	case FixVCSLogin, FixGuardHistory:
		return seal(Plan{Groups: []PlanGroup{}, Notes: []Note{}, Steps: []PlanStep{{ID: req.ID}}, Supported: true}), nil
	case FixGuardHooks:
		return seal(Plan{Groups: []PlanGroup{}, Notes: []Note{}, Steps: []PlanStep{{ID: req.ID}}, Mutates: true, Supported: true}), nil
	case FixCleanupLegacy:
		return s.PlanCleanup(ctx, req.Root, 0)
	case FixDesktop:
		return s.planDesktopFix()
	case FixEngramInstall:
		return s.PlanEngram(ctx)
	}
	return Plan{}, errUnknownFix
}

// RunFix runs a fix. Mutating fixes need the id PlanFix returned.
func (s *Service) RunFix(ctx context.Context, req FixRequest, planID string, p Progress) (FixResult, error) {
	res := FixResult{ID: req.ID}
	fresh, err := s.PlanFix(ctx, req)
	if err != nil {
		return res, err
	}
	if fresh.Mutates {
		if err := checkPlan(planID, fresh); err != nil {
			return res, err
		}
	}
	switch req.ID {
	case FixRuntimeRepair, FixRuntimeConnect:
		r, err := s.ApplyIntegration(ctx, IntegrationRequest{Root: req.Root, Runtime: req.Args["runtime"], Op: OpApply}, planID, p)
		res.Runtime, res.OK = &r, err == nil
		return res, err
	case FixVCSLogin:
		si, err := s.CheckSignIn(ctx, req.Root)
		res.SignIn, res.OK = &si, err == nil && si.OK
		return res, err
	case FixGuardHistory:
		if s.d.GuardHistory == nil {
			return res, ErrNotAvailable
		}
		g, err := s.d.GuardHistory(ctx, req.Root)
		res.Guard, res.OK = &g, err == nil
		return res, err
	case FixGuardHooks:
		err := s.SetGuardHooks(ctx, req.Root, true)
		res.OK = err == nil
		return res, err
	case FixCleanupLegacy:
		r, err := s.ApplyCleanup(ctx, req.Root, 0, planID)
		res.Removed, res.OK = r, err == nil
		return res, err
	case FixEngramInstall:
		if !fresh.Supported {
			return res, ErrUnsupportedOS
		}
		if !fresh.Mutates { // already there
			res.OK = true
			return res, nil
		}
		steps, err := s.ApplyEngram(ctx, planID, p)
		res.OK = err == nil
		if len(steps) > 0 {
			res.Message = steps[0].Message
		}
		return res, err
	case FixDesktop:
		r := s.reapplyDesktop(p)
		res.OK, res.Message = r.Status == StatusOK, r.Message
		if r.Status == StatusUnsupported {
			return res, ErrUnsupportedOS
		}
		if r.Status == StatusError {
			return res, errors.New(r.Message)
		}
		return res, nil
	}
	return res, errUnknownFix
}

// desktopRepairOptions keep what the user already has and make sure the Start
// menu entry and PATH are in place.
func (s *Service) desktopRepairOptions() desktop.Options {
	st := s.DesktopStatus()
	return desktop.Options{InstallDir: st.InstallDir, Version: s.d.Version, AddToPath: true, StartMenu: true, DesktopShortcut: st.DesktopLink}
}

func (s *Service) planDesktopFix() (Plan, error) {
	steps, supported, err := s.desktopSteps(s.desktopRepairOptions())
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Groups: []PlanGroup{}, Notes: []Note{}, Steps: []PlanStep{}, Supported: supported, Mutates: supported}
	for _, st := range steps {
		p.Steps = append(p.Steps, PlanStep{ID: "desktop." + st.ID, Title: st.Title})
	}
	if !supported {
		p.Unsupports = []string{"desktop"}
	}
	return seal(p), nil
}

func (s *Service) reapplyDesktop(p Progress) StepResult {
	return s.applyDesktop(s.desktopRepairOptions(), p, 10, 100)
}
