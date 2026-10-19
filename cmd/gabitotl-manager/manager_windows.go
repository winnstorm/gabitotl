//go:build windows

package main

import (
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

// CodeGraph reports the required CodeGraph component and its MCP registration.
func (a *App) CodeGraph() (gui.CodeGraphStatus, error) { return a.svc.CodeGraph(a.ctx) }

// PlanCodeGraph shows what repairing or upgrading CodeGraph would do.
func (a *App) PlanCodeGraph() (gui.Plan, error) { return a.svc.PlanCodeGraph(a.ctx) }

// ApplyCodeGraph runs that plan.
func (a *App) ApplyCodeGraph(planID string) ([]gui.StepResult, error) {
	return a.svc.ApplyCodeGraph(a.ctx, planID, a.progress())
}

// PlanUninstall shows what an uninstall would remove and keep.
func (a *App) PlanUninstall(o gui.UninstallOptions) (gui.UninstallPlan, error) {
	return a.svc.PlanUninstall(a.ctx, o)
}

// Uninstall removes what the plan listed.
func (a *App) Uninstall(o gui.UninstallOptions, planID string) (gui.UninstallResult, error) {
	return a.svc.Uninstall(a.ctx, o, planID, a.progress())
}
