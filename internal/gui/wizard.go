package gui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	"github.com/winnstorm/gabitotl/internal/persona"
)

// Gentle AI actions of the detect screen.
const (
	ActionMigrate = "migrate"
	ActionKeep    = "keep"
	ActionReview  = "review"
)

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Detect builds the inventory of the second wizard screen. It is read-only.
// The wizard is global: root is ignored, like in PlanInstall and Install.
func (s *Service) Detect(ctx context.Context, _ string) (DetectResult, error) {
	root := ""
	env, err := s.env(root)
	if err != nil {
		return DetectResult{}, err
	}
	m := msgs(env, "")
	inv := installer.BuildInventory(ctx, env, nil)
	r := DetectResult{
		Root: inv.Project.Root, ProjectName: inv.Project.Name, ProjectReady: inv.Project.Exists,
		OS: inv.Platform.OS, Arch: inv.Platform.Arch, PackageMgr: inv.Platform.PackageManager,
		Container: inv.Server.Engine, Language: string(gabitotl.ActivePersonaLang(env.Home)),
		Git:      GitInfo{Found: inv.Platform.Git, Path: inv.Platform.GitPath, Repo: inv.Project.GitRepo, Provider: inv.Project.Provider},
		Engram:   EngramInfo{Present: inv.Memory.Present(), HasBinary: inv.Memory.HasBinary, Observations: inv.Memory.Observations, CanImport: inv.Server.CanImport},
		Runtimes: []RuntimeInfo{}, Desktop: s.DesktopStatus(),
		PersonaMode: string(gabitotl.LoadPersonaMode(gabitotl.NewLayout(root), gabitotl.ActiveUserID(), env.Home)),
		UserName:    gitUserName(ctx, env, inv.Project.Root),
	}
	for _, rs := range inv.Runtimes {
		r.Runtimes = append(r.Runtimes, RuntimeInfo{Name: rs.Name, Label: rs.Label, State: string(rs.State), Usable: rs.Usable(), Conflict: len(rs.Conflicts) > 0})
	}
	if root != "" {
		r.CodeIndex = s.codeIndex().Backends(root)
	}
	if g := inv.Other.GentleAI; g.Found {
		r.Gentle = GentleInfo{Found: true, Version: g.Version, Action: ActionReview}
		for _, a := range g.Agents {
			r.Gentle.Agents = append(r.Gentle.Agents, a.ID)
		}
		// The default plan tells whether the choices that depend on Gentle AI matter.
		if pl, err := installer.BuildPlan(ctx, env, inv, installer.Options{}); err == nil {
			r.Gentle.PersonaConflict = pl.HasNote(installer.NotePersonaConflict)
			r.Gentle.GentleKiroPresent = pl.HasNote(installer.NoteGentleKiroPresent)
		}
	}
	// "Not inside a project" is a CLI warning; the global wizard never has one.
	noProject := m.Format("inv.project.none")
	for _, sec := range ui.InventorySections(m, inv) {
		part := InventoryPart{Title: sec.Title, Rows: []string{}}
		for _, l := range sec.Lines {
			if l.Text == noProject {
				continue
			}
			part.Rows = append(part.Rows, strings.TrimSpace(l.Sym+" "+l.Text))
		}
		r.Sections = append(r.Sections, part)
	}
	return r, nil
}

// gitUserName is the profile default: `git config user.name`.
func gitUserName(ctx context.Context, env installer.Env, root string) string {
	if env.Runner == nil {
		return ""
	}
	out, _, err := env.Runner.Run(ctx, root, "git", "config", "user.name")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// CheckSignIn checks the login to the Git platform of the project (the "check
// sign-in" button of the delivery screen). It never logs in.
func (s *Service) CheckSignIn(ctx context.Context, root string) (SignIn, error) {
	if s.d.VCSAuth == nil {
		return SignIn{}, ErrNotAvailable
	}
	return s.d.VCSAuth(ctx, root)
}

// installOptions validates the answers and turns them into installer options.
func installOptions(a InstallAnswers) (installer.Options, error) {
	if a.Scope != "" && a.Scope != "global" {
		return installer.Options{}, fmt.Errorf("%w: the wizard configures the runtimes globally; project scope is available from the Manager", ErrNotAvailable)
	}
	if a.PersonaMode != "" {
		if _, err := persona.ParseMode(a.PersonaMode); err != nil {
			return installer.Options{}, err
		}
	}
	if a.Lang != "" {
		if _, err := persona.ParseLang(a.Lang); err != nil {
			return installer.Options{}, err
		}
	}
	o := installer.Options{
		Source: installer.SourceInstall, Runtimes: a.Runtimes, User: a.User, UserName: a.Name,
		MigratePersona: a.MigratePersona, RetireGentleKiro: a.RetireGentleKiro, DryRun: a.DryRun,
		NoGuardHooks: true, PersonaLang: a.Lang,
	}
	if o.UserName == "" {
		o.UserName = a.User
	}
	switch a.GentleAction {
	case ActionMigrate:
		o.Source = installer.SourceGentleAI
	case ActionReview:
		return o, fmt.Errorf("%w: choose migrate or keep for Gentle AI", ErrDecisionPending)
	}
	if err := memoryOptions(&o, a.Memory); err != nil {
		return o, err
	}
	// The install is global: delivery answers become the user's global defaults
	// (runExtras), never a project vcs.md, so the installer gets none of them.
	if err := globalAnswers(a.Delivery).Validate(); err != nil {
		return o, err
	}
	return o, nil
}

var indexTools = map[string]bool{"codegraph": true, "code-review-graph": true}

// memoryOptions maps the memory screen to the installer's team options.
func memoryOptions(o *installer.Options, m MemoryAnswers) error {
	switch m.Mode {
	case "":
		return nil
	case "solo":
		o.Memory, o.Mode = gabitotl.MemoryEngram, gabitotl.ModeSolo
		return nil
	case "team":
		o.Memory, o.Mode = gabitotl.MemoryMemoryMCP, gabitotl.ModeTeam
	default:
		return fmt.Errorf("memory mode must be solo or team, got %q", m.Mode)
	}
	if m.Role != "" && m.Role != gabitotl.RoleHost && m.Role != gabitotl.RoleClient {
		return fmt.Errorf("memory role must be host or client, got %q", m.Role)
	}
	if m.Engine != "" && !slices.Contains([]string{"auto", "docker", "podman"}, m.Engine) {
		return fmt.Errorf("container engine must be auto, docker or podman, got %q", m.Engine)
	}
	o.MemoryRole, o.MemoryURL, o.MemoryTokenEnv, o.StoreToken = m.Role, m.URL, m.TokenEnv, m.StoreToken
	o.ServerEngine, o.ServerPort, o.ServerInstance = m.Engine, m.Port, m.Instance
	o.ImportEngram, o.Invites = m.ImportEngram, m.Invites
	return nil
}

// prep is the resolved context of one wizard call.
type prep struct {
	env  installer.Env
	inv  installer.Inventory
	opts installer.Options
	m    ui.Messages
	plan installer.Plan
}

func (s *Service) prepare(ctx context.Context, a InstallAnswers) (prep, error) {
	opts, err := installOptions(a)
	if err != nil {
		return prep{}, err
	}
	// The wizard never has a project: the install is global.
	env, err := s.env("")
	if err != nil {
		return prep{}, err
	}
	opts.CodeGraph = s.codegraph().Supported()
	inv := installer.BuildInventory(ctx, env, nil)
	if opts.Source == installer.SourceGentleAI && !inv.Other.GentleAI.Found {
		return prep{}, errors.New("Gentle AI was not found on this machine: nothing to migrate")
	}
	pl, err := installer.BuildPlan(ctx, env, inv, opts)
	if err != nil {
		return prep{}, err
	}
	return prep{env: env, inv: inv, opts: opts, m: msgs(env, a.Lang), plan: pl}, nil
}

// extraSteps are the steps that run after the installer plan.
func (s *Service) extraSteps(a InstallAnswers, p prep) ([]PlanStep, []Note, []string, error) {
	var steps []PlanStep
	var notes []Note
	var unsupported []string
	if cg := s.codegraphStep(p.m); cg != nil {
		steps = append(steps, *cg)
	}
	if es := s.engramStep(context.Background(), p.m, p); es != nil {
		steps = append(steps, *es)
	}
	if a.PersonaMode != "" {
		steps = append(steps, PlanStep{ID: "persona-mode", Title: p.m.Format("gui.step.persona-mode", "mode", a.PersonaMode)})
	}
	if da := globalAnswers(a.Delivery); !da.Empty() {
		steps = append(steps, PlanStep{ID: "delivery-global", Title: p.m.Format("gui.step.delivery-global", "automation", da.Automation)})
	}
	if a.Windows.InstallDir != "" || a.Windows.StartMenu || a.Windows.DesktopShortcut || a.Windows.AddToPath {
		do := s.desktopOptions(a.Windows)
		ds, supported, err := s.desktopSteps(do)
		if err != nil {
			return nil, nil, nil, err
		}
		if !supported {
			unsupported = append(unsupported, "desktop")
		}
		dsteps, dnotes := s.desktopPlanSteps(p.m, do, ds)
		steps = append(steps, dsteps...)
		notes = append(notes, dnotes...)
	}
	return steps, notes, unsupported, nil
}

func (s *Service) planFrom(a InstallAnswers, p prep) (Plan, error) {
	steps, extra, unsupported, err := s.extraSteps(a, p)
	if err != nil {
		return Plan{}, err
	}
	notes := append(noteList(p.m, p.plan.Notes), extra...)
	for _, w := range p.inv.Memory.Warnings {
		notes = append(notes, Note{Code: "warn." + w.Code, Text: p.m.Format("warn."+w.Code, "detail", w.Detail)})
	}
	return seal(Plan{
		Groups: planGroups(p.m, p.env, p.plan), Notes: notes, Steps: steps, Supported: true,
		Mutates: p.plan.Mutates() || len(steps) > 0, Snapshot: p.plan.SnapshotID, Unsupports: unsupported,
	}), nil
}

// PlanInstall shows what the wizard would do. It writes nothing.
func (s *Service) PlanInstall(ctx context.Context, a InstallAnswers) (Plan, error) {
	p, err := s.prepare(ctx, a)
	if err != nil {
		return Plan{}, err
	}
	return s.planFrom(a, p)
}

// phaseWeight places the installer phases in the first 70% of the progress bar.
func phasePercent(ph installer.Phase) int {
	i := slices.Index(installer.PhaseOrder, ph)
	if i < 0 {
		return 0
	}
	return 5 + 65*i/len(installer.PhaseOrder)
}

// observer maps installer events to GUI events in the person's language.
func observer(m ui.Messages, p Progress) installer.Observer {
	return func(e installer.Event) {
		switch e.Status {
		case "start":
			if msg, ok := m["phase."+string(e.Phase)]; ok {
				p.emit(string(e.Phase), "running", msg, phasePercent(e.Phase))
			}
		case "warn":
			p.emit(string(e.Phase), "warn", strings.TrimSpace(e.Step+" "+e.Detail), phasePercent(e.Phase))
		case "error":
			p.emit(string(e.Phase), "error", strings.TrimSpace(e.Step+" "+e.Detail), phasePercent(e.Phase))
		}
	}
}

// resultOf copies a run into its DTO.
func resultOf(m ui.Messages, res installer.Result) InstallResult {
	r := InstallResult{
		DryRun: res.DryRun, Skipped: res.Skipped, RolledBack: res.RolledBack, Applied: append([]string{}, res.Applied...),
		Checks: []Check{}, Warnings: append([]string{}, res.Warnings...), Notices: noteList(m, res.Notices), Steps: []StepResult{},
	}
	if res.Snapshot != nil {
		r.Snapshot = res.Snapshot.Dir
	}
	for _, c := range res.Checks {
		r.Checks = append(r.Checks, Check{Group: c.Group, Name: c.Name, OK: c.OK, Detail: c.Detail})
	}
	for _, t := range res.Tokens {
		r.Tokens = append(r.Tokens, IssuedToken{User: t.User, Token: t.Token})
	}
	return r
}

// Install runs the wizard. planID must be the id PlanInstall returned for the
// same answers. The installer run keeps its guarantees (snapshot first, verify,
// rollback); the optional steps after it report their own outcome in Steps and
// never undo a verified install.
func (s *Service) Install(ctx context.Context, a InstallAnswers, planID string, p Progress) (InstallResult, error) {
	pr, err := s.prepare(ctx, a)
	if err != nil {
		return InstallResult{}, err
	}
	fresh, err := s.planFrom(a, pr)
	if err != nil {
		return InstallResult{}, err
	}
	if err := checkPlan(planID, fresh); err != nil {
		return InstallResult{}, err
	}
	// CodeGraph is required: install it first, so a failure stops the wizard
	// before any agent is configured to start it.
	var cgStep []StepResult
	if !a.DryRun && s.codegraph().Supported() {
		r := s.applyCodeGraph(ctx, pr.m, p, 1, 5)
		cgStep = append(cgStep, r)
		if r.Status == StatusError {
			err := errors.New(r.Message)
			return InstallResult{Error: r.Message, Steps: cgStep, Applied: []string{}, Checks: []Check{}, Warnings: []string{}, Notices: []Note{}}, err
		}
	}
	// Engram is installed next when the chosen memory is Engram and it is
	// missing; the run then sees the binary and registers its server.
	pr, engStep, err := s.ensureEngram(ctx, a.DryRun, func() (prep, error) { return s.prepare(ctx, a) }, pr, p, 5, 12)
	if err != nil {
		return InstallResult{Error: err.Error(), Steps: append(cgStep, engStep...), Applied: []string{}, Checks: []Check{}, Warnings: []string{}, Notices: []Note{}}, err
	}
	cgStep = append(cgStep, engStep...)
	res, err := installer.Run(ctx, pr.env, pr.inv, pr.plan, observer(pr.m, p), installer.Hooks{})
	out := resultOf(pr.m, res)
	if err != nil {
		out.Error = err.Error()
		return out, err
	}
	if res.DryRun {
		return out, nil
	}
	out.Steps = append(cgStep, s.runExtras(ctx, a, pr, p)...)
	p.emit("done", "done", pr.m.Format("result.done"), 100)
	return out, nil
}

// runExtras applies the optional wizard steps after a verified install.
func (s *Service) runExtras(ctx context.Context, a InstallAnswers, pr prep, p Progress) []StepResult {
	var out []StepResult
	add := func(id string, err error) {
		r := StepResult{ID: id, Status: StatusOK}
		if err != nil {
			r.Status, r.Message = StatusError, err.Error()
		}
		p.emit(id, map[bool]string{true: "done", false: "error"}[err == nil], id, 80)
		out = append(out, r)
	}
	if a.PersonaMode != "" {
		mode, _ := persona.ParseMode(a.PersonaMode)
		_, err := gabitotl.SavePersonaMode(pr.env.ProjectRoot, gabitotl.ActiveUserID(), pr.env.Home, mode)
		add("persona-mode", err)
	}
	if da := globalAnswers(a.Delivery); !da.Empty() {
		_, err := s.SetGlobalDelivery(GlobalDeliveryAnswers{Automation: da.Automation, MergeMethod: da.MergeMethod, DeleteBranch: a.Delivery.DeleteBranch, Draft: a.Delivery.Draft})
		add("delivery-global", err)
	}
	if a.Windows.InstallDir != "" || a.Windows.StartMenu || a.Windows.DesktopShortcut || a.Windows.AddToPath {
		r := s.applyDesktop(s.desktopOptions(a.Windows), p, 85, 100)
		if r.Status == StatusUnsupported {
			p.emit("desktop", "skipped", pr.m.Format("gui.desktop.unsupported"), 100)
		}
		out = append(out, r)
	}
	return out
}

// globalAnswers is the part of the delivery screen that is a global default. The
// platform and the token variable are not: projects detect their platform from
// the remote and tokens live in the credential store.
func globalAnswers(d DeliveryAnswers) gabitotl.DeliveryAnswers {
	return gabitotl.DeliveryAnswers{Automation: d.Automation, MergeMethod: d.MergeMethod, DeleteBranch: d.DeleteBranch, Draft: d.Draft}
}

// saveTokenRef stores the reference (never the token) to the variable that
// holds the Git platform token, in the person's own vcs.md.
func saveTokenRef(root, name string) error {
	if !envNameRe.MatchString(name) {
		return fmt.Errorf("the token variable must be a name like BITBUCKET_TOKEN, got %q", name)
	}
	l := gabitotl.NewLayout(root)
	user := gabitotl.ActiveUserID()
	cfg, err := gabitotl.LoadVCSFor(l, user)
	if errors.Is(err, fs.ErrNotExist) {
		cfg, err = gabitotl.DefaultVCS("git"), nil
	}
	if err != nil {
		return err
	}
	cfg.Auth = "env:" + name
	return gabitotl.SaveVCSFor(l, user, cfg)
}
