package gui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"

	"github.com/winnstorm/gabitotl/internal/desktop"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	"github.com/winnstorm/gabitotl/internal/persona"
	"github.com/winnstorm/gabitotl/internal/pgmanaged"
)

// Settings is the Manager's settings screen.
type Settings struct {
	Language    string           `json:"language"`
	PersonaMode string           `json:"personaMode"`
	Memory      MemorySettings   `json:"memory"`
	Delivery    DeliverySettings `json:"delivery"`
	Guard       GuardSettings    `json:"guard"`
	CodeIndex   []IndexBackend   `json:"codeIndex"`
	Desktop     DesktopStatus    `json:"desktop"`
}

// MemorySettings is memory.md as the page shows it.
type MemorySettings struct {
	Configured bool   `json:"configured"` // the project has a memory.md
	Provider   string `json:"provider"`
	Mode       string `json:"mode"`
	Role       string `json:"role"`
	URL        string `json:"url"`
	TokenEnv   string `json:"tokenEnv"`
	Instance   string `json:"instance"`
}

// DeliverySettings is vcs.md as the page shows it.
type DeliverySettings struct {
	Configured   bool   `json:"configured"`
	Provider     string `json:"provider"`
	Automation   string `json:"automation"`
	MergeMethod  string `json:"mergeMethod"`
	DeleteBranch bool   `json:"deleteBranch"`
	Draft        bool   `json:"draft"`
	Auth         string `json:"auth"` // env:NAME reference, never a token
}

// GuardSettings is the authorship guard of the project.
type GuardSettings struct {
	Mode           string `json:"mode"`
	HooksAnswer    string `json:"hooksAnswer"`    // ask | installed | declined
	HooksInstalled bool   `json:"hooksInstalled"` // the remembered answer is "installed"
}

// ChangeResult reports a settings change. Files are the files written.
type ChangeResult struct {
	Files []string `json:"files"`
	Notes []Note   `json:"notes"`
}

// Settings reads every setting the Manager can change.
func (s *Service) Settings(ctx context.Context, root string) (Settings, error) {
	env, err := s.env(root)
	if err != nil {
		return Settings{}, err
	}
	user := gabitotl.ActiveUserID()
	out := Settings{
		Language:    string(gabitotl.ActivePersonaLang(env.Home)),
		PersonaMode: string(gabitotl.LoadPersonaMode(gabitotl.NewLayout(root), user, env.Home)),
		Desktop:     s.DesktopStatus(),
		CodeIndex:   []IndexBackend{},
		Guard:       GuardSettings{Mode: gabitotl.DefaultWorkflow().BrandingGuard, HooksAnswer: gabitotl.HooksAsk},
	}
	if root == "" || !gabitotl.Exists(root) {
		return out, nil
	}
	l := gabitotl.NewLayout(root)
	out.CodeIndex = s.codeIndex().Backends(root)
	if c, err := gabitotl.LoadMemory(l); err == nil {
		out.Memory = MemorySettings{Configured: true, Provider: c.Provider, Mode: c.Mode, Role: c.Role, URL: c.URL, TokenEnv: envRef(c.Token), Instance: c.Instance}
	}
	switch c, err := gabitotl.LoadVCSFor(l, user); {
	case err == nil:
		out.Delivery = DeliverySettings{
			Configured: c.IsConfigured(), Provider: c.Provider, Automation: c.AutomationLevel(), MergeMethod: c.EffectiveMergeMethod(),
			DeleteBranch: c.DeleteBranch, Draft: c.DraftPRs, Auth: c.Auth,
		}
	case errors.Is(err, fs.ErrNotExist):
	default:
		return out, err
	}
	if w, err := gabitotl.LoadWorkflow(l); err == nil {
		out.Guard = GuardSettings{Mode: w.BrandingGuard, HooksAnswer: w.GuardHooks, HooksInstalled: w.GuardHooks == gabitotl.HooksInstalled}
	}
	return out, nil
}

func envRef(ref string) string {
	if len(ref) > 4 && ref[:4] == "env:" {
		return ref[4:]
	}
	return ref
}

// SetPersona saves the language and the chat mode. The language is global; the
// mode is personal to this person in this project. Runtimes keep the persona
// text of the old language until the doctor's repair re-applies them.
func (s *Service) SetPersona(root, lang, mode string) (ChangeResult, error) {
	env, err := s.env(root)
	if err != nil {
		return ChangeResult{}, err
	}
	res := ChangeResult{Files: []string{}, Notes: []Note{}}
	if lang != "" {
		l, err := persona.ParseLang(lang)
		if err != nil {
			return res, err
		}
		p, err := gabitotl.SavePersonaLang(env.Home, l)
		if err != nil {
			return res, err
		}
		res.Files = append(res.Files, p)
		res.Notes = append(res.Notes, Note{Code: "persona.reapply", Text: msgs(env, "").Format("gui.note.persona-reapply")})
	}
	if mode != "" {
		m, err := persona.ParseMode(mode)
		if err != nil {
			return res, err
		}
		p, err := gabitotl.SavePersonaMode(root, gabitotl.ActiveUserID(), env.Home, m)
		if err != nil {
			return res, err
		}
		res.Files = append(res.Files, p)
	}
	return res, nil
}

// SetDelivery saves the delivery answers where the existing rule puts them (the
// team file or the person's own) and, for Bitbucket, the token variable name.
func (s *Service) SetDelivery(root string, d DeliveryAnswers) (ChangeResult, error) {
	root, err := checkRoot(root)
	if err != nil {
		return ChangeResult{}, err
	}
	if !gabitotl.Exists(root) {
		return ChangeResult{}, errors.New("there is no .gabitotl here: install GabitoTL in the project first")
	}
	if d.Provider == "none" {
		d.Provider = ""
	}
	da := gabitotl.DeliveryAnswers{Provider: d.Provider, Automation: d.Automation, MergeMethod: d.MergeMethod, DeleteBranch: d.DeleteBranch, Draft: d.Draft}
	if err := da.Validate(); err != nil {
		return ChangeResult{}, err
	}
	res := ChangeResult{Files: []string{}, Notes: []Note{}}
	if !da.Empty() {
		r, err := gabitotl.SaveDelivery(gabitotl.NewLayout(root), gabitotl.ActiveUserID(), da)
		if err != nil {
			return res, err
		}
		res.Files = append(res.Files, r.Path)
	}
	if d.TokenEnv != "" {
		if err := saveTokenRef(root, d.TokenEnv); err != nil {
			return res, err
		}
	}
	return res, nil
}

// SetGuardHooks installs or removes the authorship guard Git hooks.
func (s *Service) SetGuardHooks(ctx context.Context, root string, install bool) error {
	if s.d.GuardHooks == nil {
		return ErrNotAvailable
	}
	root, err := checkRoot(root)
	if err != nil {
		return err
	}
	return s.d.GuardHooks(ctx, root, install, s.installedCLI())
}

// SetCodeIndex indexes the project with the chosen tool.
func (s *Service) SetCodeIndex(ctx context.Context, root, tool string) error {
	if !indexTools[tool] {
		return fmt.Errorf("unknown code index %q (want codegraph or code-review-graph)", tool)
	}
	root, err := checkRoot(root)
	if err != nil {
		return err
	}
	env, err := s.env(root)
	if err != nil {
		return err
	}
	return s.codeIndex().Setup(ctx, env.Runner, root, tool)
}

// memoryRun resolves the install-flow run behind a memory change, the same one
// `gabitotl memory use` performs: snapshot, team setup, verify, rollback.
func (s *Service) memoryRun(ctx context.Context, root string, m MemoryAnswers) (prep, error) {
	if root == "" || !gabitotl.Exists(root) {
		return prep{}, errors.New("there is no .gabitotl here: install GabitoTL in the project first")
	}
	if m.Mode == "" {
		return prep{}, errors.New("choose solo or team memory")
	}
	o := installer.Options{Source: installer.SourceInstall}
	if err := memoryOptions(&o, m); err != nil {
		return prep{}, err
	}
	env, err := s.env(root)
	if err != nil {
		return prep{}, err
	}
	inv := installer.BuildInventory(ctx, env, nil)
	pl, err := installer.BuildPlan(ctx, env, inv, o)
	if err != nil {
		return prep{}, err
	}
	return prep{env: env, inv: inv, opts: o, m: msgs(env, ""), plan: pl}, nil
}

// PlanMemory shows what changing the memory provider or its team setup does.
func (s *Service) PlanMemory(ctx context.Context, root string, m MemoryAnswers) (Plan, error) {
	pr, err := s.memoryRun(ctx, root, m)
	if err != nil {
		return Plan{}, err
	}
	return s.planFrom(InstallAnswers{}, pr)
}

// ApplyMemory runs the memory change through the installer flow.
func (s *Service) ApplyMemory(ctx context.Context, root string, m MemoryAnswers, planID string, p Progress) (InstallResult, error) {
	pr, err := s.memoryRun(ctx, root, m)
	if err != nil {
		return InstallResult{}, err
	}
	fresh, err := s.planFrom(InstallAnswers{}, pr)
	if err != nil {
		return InstallResult{}, err
	}
	if err := checkPlan(planID, fresh); err != nil {
		return InstallResult{}, err
	}
	res, err := installer.Run(ctx, pr.env, pr.inv, pr.plan, observer(pr.m, p), installer.Hooks{})
	out := resultOf(pr.m, res)
	if err != nil {
		out.Error = err.Error()
		return out, err
	}
	p.emit("done", "done", pr.m.Format("result.done"), 100)
	return out, nil
}

// Snapshot is one migration snapshot the Manager can restore.
type Snapshot struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	Files     int    `json:"files"`
	Dir       string `json:"dir"`
}

// Snapshots lists the snapshots, oldest first.
func (s *Service) Snapshots(root string) ([]Snapshot, error) {
	env, err := s.env(root)
	if err != nil {
		return nil, err
	}
	snaps, err := installer.ListSnapshots(env)
	if err != nil {
		return nil, err
	}
	out := []Snapshot{}
	for _, sn := range snaps {
		out = append(out, Snapshot{ID: sn.ID, Source: sn.Manifest.Source, Status: sn.Manifest.Status, CreatedAt: sn.Manifest.CreatedAt, Files: len(sn.Manifest.Files), Dir: sn.Dir})
	}
	return out, nil
}

// PlanRestore shows which files restoring snapshot id would change.
func (s *Service) PlanRestore(root, id string) (Plan, error) {
	env, err := s.env(root)
	if err != nil {
		return Plan{}, err
	}
	m := msgs(env, "")
	_, changes, err := installer.PlanRestore(env, id)
	if err != nil {
		return Plan{}, err
	}
	paths := ui.Paths{Home: env.Home, Root: env.ProjectRoot}
	groups := map[string]*PlanGroup{}
	var order []string
	for _, c := range changes {
		g, ok := groups[c.Action]
		if !ok {
			g = &PlanGroup{Kind: c.Action, Label: m.Format("restore.change." + c.Action)}
			groups[c.Action], order = g, append(order, c.Action)
		}
		g.Lines = append(g.Lines, paths.Short(c.Path))
	}
	p := Plan{Groups: []PlanGroup{}, Notes: []Note{{Code: "restore.note", Text: m.Format("restore.note")}}, Steps: []PlanStep{}, Mutates: len(changes) > 0, Supported: true, Snapshot: id}
	for _, k := range order {
		p.Groups = append(p.Groups, *groups[k])
	}
	return seal(p), nil
}

// RestoreResult is what a restore did.
type RestoreResult struct {
	Changed []string `json:"changed"`
	Backup  string   `json:"backup"` // where the files it replaced were saved first
}

// Restore puts snapshot id back. The installer saves the current version of
// every file it replaces, so a restore is itself reversible.
func (s *Service) Restore(root, id, planID string, p Progress) (RestoreResult, error) {
	fresh, err := s.PlanRestore(root, id)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := checkPlan(planID, fresh); err != nil {
		return RestoreResult{}, err
	}
	env, err := s.env(root)
	if err != nil {
		return RestoreResult{}, err
	}
	obs := func(e installer.Event) {
		p.emit(string(e.Phase), "running", e.Step, 50)
	}
	changed, backup, err := installer.Restore(env, id, obs)
	p.emit("restore", statusOf(err), id, 100)
	return RestoreResult{Changed: append([]string{}, changed...), Backup: backup}, err
}

// PlanCleanup lists legacy snapshots and backups older than days (0: the
// default retention).
func (s *Service) PlanCleanup(_ context.Context, root string, days int) (Plan, error) {
	env, err := s.env(root)
	if err != nil {
		return Plan{}, err
	}
	m := msgs(env, "")
	if days < 1 {
		days = installer.DefaultRetentionDays
	}
	items, err := installer.PlanCleanup(env, days)
	if err != nil {
		return Plan{}, err
	}
	paths := ui.Paths{Home: env.Home, Root: env.ProjectRoot}
	g := PlanGroup{Kind: "REMOVE", Label: m.Format("cleanup.title", "days", strconv.Itoa(days))}
	for _, it := range items {
		g.Lines = append(g.Lines, m.Format("cleanup.item", "kind", it.Kind, "path", paths.Short(it.Path), "age", strconv.Itoa(it.AgeDays)))
	}
	p := Plan{Groups: []PlanGroup{}, Notes: []Note{{Code: "cleanup.note", Text: m.Format("cleanup.note")}}, Steps: []PlanStep{}, Mutates: len(items) > 0, Supported: true}
	if len(g.Lines) > 0 {
		p.Groups = append(p.Groups, g)
	}
	return seal(p), nil
}

// ApplyCleanup deletes the expired legacy material the plan listed.
func (s *Service) ApplyCleanup(ctx context.Context, root string, days int, planID string) ([]string, error) {
	fresh, err := s.PlanCleanup(ctx, root, days)
	if err != nil {
		return nil, err
	}
	if err := checkPlan(planID, fresh); err != nil {
		return nil, err
	}
	env, err := s.env(root)
	if err != nil {
		return nil, err
	}
	if days < 1 {
		days = installer.DefaultRetentionDays
	}
	items, err := installer.PlanCleanup(env, days)
	if err != nil {
		return nil, err
	}
	removed, err := installer.ApplyCleanup(env, items)
	return append([]string{}, removed...), err
}

// UninstallOptions say what an uninstall covers.
type UninstallOptions struct {
	Root     string   `json:"root"`
	Runtimes []string `json:"runtimes"` // empty: every runtime (and the guard hooks)
	// RemoveMemoryServer also removes the managed team memory container; the
	// data volume is kept unless PurgeMemoryData is set and PurgeConfirm is the
	// volume name the plan showed (the same typed confirmation the CLI asks).
	RemoveMemoryServer bool   `json:"removeMemoryServer"`
	PurgeMemoryData    bool   `json:"purgeMemoryData"`
	PurgeConfirm       string `json:"purgeConfirm"`
	// Desktop also removes the Windows integration (shortcuts, PATH entry, Apps
	// entry and the app files once the app closes).
	Desktop    bool   `json:"desktop"`
	InstallDir string `json:"installDir"`
}

// UninstallPlan is the review of an uninstall.
type UninstallPlan struct {
	Plan
	Runtimes     []string `json:"runtimes"`
	GuardHooks   []string `json:"guardHooks"`
	MemoryServer string   `json:"memoryServer"` // container name, "" when not applicable
	PurgeVolume  string   `json:"purgeVolume"`  // type this to confirm a data purge
}

// teamManager returns the managed server of the project, if it has one.
func (s *Service) teamManager(env installer.Env) *pgmanaged.Manager {
	if env.Team.Manager == nil || env.ProjectRoot == "" {
		return nil
	}
	cfg, err := gabitotl.LoadMemory(gabitotl.NewLayout(env.ProjectRoot))
	if err != nil || cfg.Provider != gabitotl.MemoryMemoryMCP || cfg.Role != gabitotl.RoleHost {
		return nil
	}
	m, err := env.Team.Manager(cfg.Instance, "auto", 0)
	if err != nil {
		return nil
	}
	return m
}

// PlanUninstall shows everything an uninstall would remove and keep.
func (s *Service) PlanUninstall(_ context.Context, o UninstallOptions) (UninstallPlan, error) {
	env, err := s.env(o.Root)
	if err != nil {
		return UninstallPlan{}, err
	}
	m := msgs(env, "")
	up, err := installer.PlanUninstall(env, o.Runtimes)
	if err != nil {
		return UninstallPlan{}, err
	}
	paths := ui.Paths{Home: env.Home, Root: env.ProjectRoot}
	out := UninstallPlan{Runtimes: append([]string{}, up.Runtimes...), GuardHooks: []string{}}
	out.Groups, out.Notes, out.Steps, out.Supported = []PlanGroup{}, []Note{}, []PlanStep{}, true
	g := PlanGroup{Kind: "REMOVE", Label: m.Format("uninstall.remove")}
	for _, n := range up.Runtimes {
		for _, f := range up.Files[n] {
			g.Lines = append(g.Lines, m.Format("uninstall.remove.item", "name", n, "detail", paths.Short(f)))
		}
	}
	for _, h := range up.GuardHooks {
		g.Lines = append(g.Lines, m.Format("uninstall.remove.item", "name", "git", paths.Short(h.Path)))
		out.GuardHooks = append(out.GuardHooks, h.Path)
		if h.Chained {
			out.Notes = append(out.Notes, Note{Code: "uninstall.keep.userhook", Text: m.Format("uninstall.keep.userhook", "detail", paths.Short(h.Path))})
		}
	}
	if len(g.Lines) > 0 {
		out.Groups = append(out.Groups, g)
	}
	for _, k := range up.Preserved {
		out.Notes = append(out.Notes, Note{Code: "keep." + k, Text: m.Format("uninstall.keep." + k)})
	}
	out.Mutates = !up.Empty()
	if o.RemoveMemoryServer {
		if mgr := s.teamManager(env); mgr != nil {
			out.MemoryServer, out.PurgeVolume = mgr.Cfg.Container, mgr.Cfg.Volume
			out.Steps = append(out.Steps, PlanStep{ID: "memory-server", Title: m.Format("gui.step.memory-server", "name", mgr.Cfg.Container)})
			if o.PurgeMemoryData {
				out.Steps = append(out.Steps, PlanStep{ID: "memory-purge", Title: m.Format("gui.step.memory-purge", "name", mgr.Cfg.Volume)})
			}
			out.Mutates = true
		}
	}
	if o.Desktop {
		ds := desktop.Options{InstallDir: o.InstallDir}
		if s.d.Desktop == nil {
			out.Unsupports = append(out.Unsupports, "desktop")
		} else if steps, err := s.d.Desktop.UninstallPlan(ds); err != nil {
			if !isUnsupported(err) {
				return out, err
			}
			out.Unsupports = append(out.Unsupports, "desktop")
		} else {
			for _, st := range steps {
				out.Steps = append(out.Steps, PlanStep{ID: "desktop." + st.ID, Title: st.Title})
			}
			out.Mutates = true
		}
	}
	out.Plan = seal(out.Plan)
	return out, nil
}

// UninstallResult is what an uninstall did.
type UninstallResult struct {
	Backup  string       `json:"backup,omitempty"`
	Removed []string     `json:"removed"`
	Manual  []string     `json:"manual"`
	Steps   []StepResult `json:"steps"`
}

// Uninstall removes what the plan listed. The runtime part goes first and saves
// a backup; the memory server and the Windows integration follow and report
// their own outcome in Steps.
func (s *Service) Uninstall(ctx context.Context, o UninstallOptions, planID string, p Progress) (UninstallResult, error) {
	fresh, err := s.PlanUninstall(ctx, o)
	if err != nil {
		return UninstallResult{}, err
	}
	if err := checkPlan(planID, fresh.Plan); err != nil {
		return UninstallResult{}, err
	}
	if o.PurgeMemoryData && o.RemoveMemoryServer && fresh.PurgeVolume != "" && o.PurgeConfirm != fresh.PurgeVolume {
		return UninstallResult{}, errors.New("confirmation did not match the volume name: nothing was removed")
	}
	env, err := s.env(o.Root)
	if err != nil {
		return UninstallResult{}, err
	}
	out := UninstallResult{Removed: []string{}, Manual: []string{}, Steps: []StepResult{}}
	p.emit("runtimes", "running", "", 10)
	res, err := installer.Uninstall(env, o.Runtimes, nil)
	for _, files := range res.Removed {
		out.Removed = append(out.Removed, files...)
	}
	out.Removed = append(out.Removed, res.GuardRemoved...)
	out.Manual, out.Backup = append(out.Manual, res.Manual...), res.Backup
	if err != nil {
		p.emit("runtimes", "error", err.Error(), 50)
		return out, err
	}
	p.emit("runtimes", "done", "", 50)
	if o.RemoveMemoryServer && fresh.MemoryServer != "" {
		out.Steps = append(out.Steps, s.removeServer(ctx, env, o, p))
	}
	if o.Desktop && s.d.Desktop != nil {
		r := StepResult{ID: "desktop", Status: StatusOK}
		err := s.d.Desktop.Uninstall(desktop.Options{InstallDir: o.InstallDir}, func(st desktop.Step) {
			p.emit("desktop."+st.ID, "running", st.Title, 85)
		})
		switch {
		case isUnsupported(err):
			r.Status = StatusUnsupported
		case err != nil:
			r.Status, r.Message = StatusError, err.Error()
		}
		out.Steps = append(out.Steps, r)
	}
	p.emit("done", "done", "", 100)
	return out, nil
}

func (s *Service) removeServer(ctx context.Context, env installer.Env, o UninstallOptions, p Progress) StepResult {
	r := StepResult{ID: "memory-server", Status: StatusOK}
	mgr := s.teamManager(env)
	if mgr == nil {
		r.Status = StatusSkipped
		return r
	}
	p.emit("memory-server", "running", mgr.Cfg.Container, 60)
	res, err := mgr.Remove(ctx, pgmanaged.RemoveOptions{Purge: o.PurgeMemoryData, Confirmed: o.PurgeMemoryData})
	if err != nil {
		r.Status, r.Message = StatusError, err.Error()
		return r
	}
	r.Message = fmt.Sprint(res.Removed)
	return r
}
