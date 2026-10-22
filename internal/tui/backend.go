// Package tui is the full-screen presentation of the installer (Bubble Tea,
// Lip Gloss, Huh). It holds no installer logic: detection, planning and the
// apply pipeline stay in internal/installer and are reached only through the
// narrow Backend interface; wording comes from the replaceable messages file of
// internal/installer/ui.
package tui

import (
	"context"

	"github.com/winnstorm/gabitotl/internal/installer"
)

// Backend is everything the TUI needs from the installer pipeline.
type Backend interface {
	// Detect runs the read-only detection phases.
	Detect(ctx context.Context) installer.Inventory
	// BuildPlan is pure: it writes nothing.
	BuildPlan(ctx context.Context, inv installer.Inventory, opts installer.Options) (installer.Plan, error)
	// Run applies the plan with the pipeline's own order and rollback.
	Run(ctx context.Context, inv installer.Inventory, pl installer.Plan, obs installer.Observer, hooks installer.Hooks) (installer.Result, error)
}

// NewBackend adapts an installer.Env to Backend.
func NewBackend(env installer.Env) Backend { return envBackend{env} }

type envBackend struct{ env installer.Env }

func (b envBackend) Detect(ctx context.Context) installer.Inventory {
	return installer.BuildInventory(ctx, b.env, nil)
}

func (b envBackend) BuildPlan(ctx context.Context, inv installer.Inventory, o installer.Options) (installer.Plan, error) {
	return installer.BuildPlan(ctx, b.env, inv, o)
}

func (b envBackend) Run(ctx context.Context, inv installer.Inventory, pl installer.Plan, obs installer.Observer, h installer.Hooks) (installer.Result, error) {
	// Engram first when the project uses it and it is missing; a failure only
	// leaves the install as it was (the plan notes the missing binary).
	if inv2, pl2, _, err := installer.EnsureEngram(ctx, b.env, inv, pl, nil); err == nil {
		inv, pl = inv2, pl2
	}
	return installer.Run(ctx, b.env, inv, pl, obs, h)
}
