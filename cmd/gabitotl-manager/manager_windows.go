//go:build windows

package main

import (
	"context"
	"os"
	"path/filepath"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/winnstorm/gabitotl/cmd/gabitotl-manager/internal/route"
	"github.com/winnstorm/gabitotl/internal/gui"
)

// The methods below are the Manager's bridge: thin pass-throughs to gui.Service.
// Long operations stream gui.Event values as "op:progress" events.

func (a *App) progress() gui.Progress {
	return func(e gui.Event) { wr.EventsEmit(a.ctx, "op:progress", e) }
}

func statePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "GabitoTL", "manager.json")
}

// Root is the project folder the Manager last worked on ("" when none).
func (a *App) Root() string { return route.LoadRoot(statePath()) }

// SetRoot remembers the project folder.
func (a *App) SetRoot(root string) error { return route.SaveRoot(statePath(), root) }

// Projects lists the projects found on this machine (Claude Code, OpenCode,
// Kiro, GabitoTL).
func (a *App) Projects() ([]gui.ProjectInfo, error) { return a.svc.Projects(a.ctx) }

// AddProject remembers a folder in the Manager's list.
func (a *App) AddProject(path string) error { return a.svc.AddProject(path) }

// ForgetProject drops a folder from the Manager's list (nothing on disk changes).
func (a *App) ForgetProject(path string) error { return a.svc.ForgetProject(path) }

// Project describes one project folder: git and platform detected from its
// remote, the effective delivery settings with their source, whether the host's
// token is stored, the code index state and the guard hooks.
func (a *App) Project(root string) (gui.ProjectDetail, error) { return a.svc.Project(a.ctx, root) }

// OtherSystems lists the other agent systems on this PC (found or not) and where
// each was looked for. It only reads.
func (a *App) OtherSystems() ([]gui.SystemInfo, error) { return a.svc.OtherSystems(a.ctx) }

// PlanProjectSetup shows what setting GabitoTL up in a folder creates.
func (a *App) PlanProjectSetup(root string) (gui.Plan, error) {
	return a.svc.PlanProjectSetup(a.ctx, root)
}

// ApplyProjectSetup creates the .gabitotl folder (the CLI's own init).
func (a *App) ApplyProjectSetup(root, planID string) (gui.ChangeResult, error) {
	return a.svc.ApplyProjectSetup(a.ctx, root, planID, a.progress())
}

// ResetProjectDelivery makes the project follow the global delivery defaults again.
func (a *App) ResetProjectDelivery(root string) (gui.ChangeResult, error) {
	return a.svc.ResetProjectDelivery(root)
}

// IndexProject indexes a project with a code index tool. CancelOperation stops it.
func (a *App) IndexProject(root, tool string) error {
	ctx, cancel := context.WithCancel(a.ctx)
	id, ok := a.setCancel(cancel)
	if !ok {
		cancel()
		return gui.ErrBusy
	}
	defer func() { a.clearCancel(id); cancel() }()
	return a.svc.IndexProject(ctx, root, tool, a.progress())
}

// CancelOperation stops the long operation that is running, if it can be stopped.
func (a *App) CancelOperation() {
	a.mu.Lock()
	cancel := a.cancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// setCancel claims the cancel slot; ok is false while another operation holds it,
// so a rejected second call never replaces the first one's cancel function.
func (a *App) setCancel(c context.CancelFunc) (id uint64, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		return 0, false
	}
	a.opID++
	a.cancel = c
	return a.opID, true
}

// clearCancel drops the cancel function only when it still is the one of op id.
func (a *App) clearCancel(id uint64) {
	a.mu.Lock()
	if a.opID == id {
		a.cancel = nil
	}
	a.mu.Unlock()
}

// CopyText puts text on the clipboard.
func (a *App) CopyText(text string) error { return wr.ClipboardSetText(a.ctx, text) }

// Doctor runs the read-only health check.
func (a *App) Doctor(root string) (gui.DoctorReport, error) { return a.svc.Doctor(a.ctx, root) }

// PlanFix shows what a doctor fix would do.
func (a *App) PlanFix(req gui.FixRequest) (gui.Plan, error) { return a.svc.PlanFix(a.ctx, req) }

// RunFix runs a doctor fix.
func (a *App) RunFix(req gui.FixRequest, planID string) (gui.FixResult, error) {
	return a.svc.RunFix(a.ctx, req, planID, a.progress())
}

// Skills lists the optional skills (GabitoDesign) with their state in each managed agent.
func (a *App) Skills() ([]gui.SkillInfo, error) { return a.svc.Skills(a.ctx) }

// PlanSkill shows what turning an optional skill on or off writes or removes.
func (a *App) PlanSkill(req gui.SkillRequest) (gui.Plan, error) { return a.svc.PlanSkill(a.ctx, req) }

// ApplySkill runs the plan PlanSkill returned.
func (a *App) ApplySkill(req gui.SkillRequest, planID string) (gui.SkillResult, error) {
	return a.svc.ApplySkill(a.ctx, req, planID, a.progress())
}

// Integrations lists the coding agents with their state.
func (a *App) Integrations(root string) ([]gui.Integration, error) {
	return a.svc.Integrations(a.ctx, root)
}

// PlanIntegration shows what connecting or removing an agent would do.
func (a *App) PlanIntegration(req gui.IntegrationRequest) (gui.Plan, error) {
	return a.svc.PlanIntegration(a.ctx, req)
}

// ApplyIntegration connects, repairs or removes an agent.
func (a *App) ApplyIntegration(req gui.IntegrationRequest, planID string) (gui.IntegrationResult, error) {
	return a.svc.ApplyIntegration(a.ctx, req, planID, a.progress())
}

// Settings reads the persona, memory, delivery, guard and code index settings.
func (a *App) Settings(root string) (gui.Settings, error) { return a.svc.Settings(a.ctx, root) }

// SetPersona saves the language and chat mode.
func (a *App) SetPersona(root, lang, mode string) (gui.ChangeResult, error) {
	return a.svc.SetPersona(root, lang, mode)
}

// SetDelivery saves the delivery settings.
func (a *App) SetDelivery(root string, d gui.DeliveryAnswers) (gui.ChangeResult, error) {
	return a.svc.SetDelivery(root, d)
}

// SetGuardHooks installs or removes the guard Git hooks.
func (a *App) SetGuardHooks(root string, install bool) error {
	return a.svc.SetGuardHooks(a.ctx, root, install)
}

// SetCodeIndex sets up the optional code index.
func (a *App) SetCodeIndex(root, tool string) error { return a.svc.SetCodeIndex(a.ctx, root, tool) }

// PlanMemory shows what a memory change would do.
func (a *App) PlanMemory(root string, m gui.MemoryAnswers) (gui.Plan, error) {
	return a.svc.PlanMemory(a.ctx, root, m)
}

// ApplyMemory applies a memory change; like Install, a failure comes back inside
// the result so the page can show the rollback.
func (a *App) ApplyMemory(root string, m gui.MemoryAnswers, planID string) (gui.InstallResult, error) {
	res, err := a.svc.ApplyMemory(a.ctx, root, m, planID, a.progress())
	if err != nil {
		res.Error = err.Error()
	}
	return res, nil
}

// Snapshots lists the backups.
func (a *App) Snapshots(root string) ([]gui.Snapshot, error) { return a.svc.Snapshots(root) }

// PlanRestore shows what restoring a backup would change.
func (a *App) PlanRestore(root, id string) (gui.Plan, error) { return a.svc.PlanRestore(root, id) }

// Restore restores a backup.
func (a *App) Restore(root, id, planID string) (gui.RestoreResult, error) {
	return a.svc.Restore(root, id, planID, a.progress())
}

// PlanCleanup shows which expired backups would be deleted.
func (a *App) PlanCleanup(root string) (gui.Plan, error) { return a.svc.PlanCleanup(a.ctx, root, 0) }

// ApplyCleanup deletes them.
func (a *App) ApplyCleanup(root, planID string) ([]string, error) {
	return a.svc.ApplyCleanup(a.ctx, root, 0, planID)
}

// PlanDesktop shows what adding Windows integration pieces would do.
func (a *App) PlanDesktop(w gui.WindowsAnswers) (gui.Plan, error) { return a.svc.PlanDesktop(w) }

// ApplyDesktop adds them.
func (a *App) ApplyDesktop(w gui.WindowsAnswers, planID string) (gui.StepResult, error) {
	return a.svc.ApplyDesktop(w, planID, a.progress())
}

// GlobalDelivery reads the delivery defaults every project starts from.
func (a *App) GlobalDelivery() (gui.GlobalDelivery, error) { return a.svc.GlobalDelivery() }

// SetGlobalDelivery saves those defaults.
func (a *App) SetGlobalDelivery(d gui.GlobalDeliveryAnswers) (gui.GlobalDelivery, error) {
	return a.svc.SetGlobalDelivery(d)
}

// Credentials lists the platforms and whether a token is stored for each. It
// never returns a token.
func (a *App) Credentials() ([]gui.CredentialInfo, error) { return a.svc.Credentials() }

// SaveCredential verifies a token with its platform and stores it only when accepted.
func (a *App) SaveCredential(platform, host, token string) (gui.CredentialResult, error) {
	return a.svc.SaveCredential(a.ctx, platform, host, token)
}

// DeleteCredential removes the stored token of a host.
func (a *App) DeleteCredential(host string) error { return a.svc.DeleteCredential(host) }

// Servers lists the registered self-hosted servers. It never returns a token.
func (a *App) Servers() ([]gui.ServerInfo, error) { return a.svc.Servers() }

// SaveServer verifies a token against a self-hosted server and registers it only when accepted.
func (a *App) SaveServer(platform, url, token string, sshHosts []string, replace bool) (gui.CredentialResult, error) {
	return a.svc.SaveServer(a.ctx, platform, url, token, sshHosts, replace)
}

// DeleteServer unregisters a self-hosted server and removes its stored token.
func (a *App) DeleteServer(host string) error { return a.svc.DeleteServer(host) }

// TestCredential asks the platform whether a stored token is still accepted.
func (a *App) TestCredential(platform, host string) (gui.CredentialResult, error) {
	return a.svc.TestCredential(a.ctx, platform, host)
}

// CodeGraph reports the required CodeGraph component and its MCP registration.
func (a *App) CodeGraph() (gui.CodeGraphStatus, error) { return a.svc.CodeGraph(a.ctx) }

// PlanCodeGraph shows what repairing or upgrading CodeGraph would do.
func (a *App) PlanCodeGraph() (gui.Plan, error) { return a.svc.PlanCodeGraph(a.ctx) }

// ApplyCodeGraph runs that plan.
func (a *App) ApplyCodeGraph(planID string) ([]gui.StepResult, error) {
	return a.svc.ApplyCodeGraph(a.ctx, planID, a.progress())
}

// Engram reports the installed Engram.
func (a *App) Engram() (gui.EngramStatus, error) { return a.svc.Engram(a.ctx) }

// PlanEngram shows what installing Engram would do.
func (a *App) PlanEngram() (gui.Plan, error) { return a.svc.PlanEngram(a.ctx) }

// ApplyEngram runs that plan.
func (a *App) ApplyEngram(planID string) ([]gui.StepResult, error) {
	return a.svc.ApplyEngram(a.ctx, planID, a.progress())
}

// PlanUninstall shows what an uninstall would remove and keep.
func (a *App) PlanUninstall(o gui.UninstallOptions) (gui.UninstallPlan, error) {
	return a.svc.PlanUninstall(a.ctx, o)
}

// Uninstall removes what the plan listed.
func (a *App) Uninstall(o gui.UninstallOptions, planID string) (gui.UninstallResult, error) {
	res, err := a.svc.Uninstall(a.ctx, o, planID, a.progress())
	if err != nil {
		a.log.Errorf("uninstall: %v", err)
	}
	for _, st := range res.Steps {
		if st.Status != gui.StatusOK {
			a.log.Infof("uninstall step %s: %s %s", st.ID, st.Status, st.Message)
		}
	}
	for _, m := range res.Manual {
		a.log.Infof("uninstall manual: %s", m)
	}
	return res, err
}
