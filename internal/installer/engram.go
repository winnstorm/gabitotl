package installer

import (
	"context"

	"github.com/winnstorm/gabitotl/internal/engraminstall"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// EngramOutcome is what EnsureEngram did.
type EngramOutcome struct {
	Installed bool   // a release was downloaded and put in place
	Version   string // the pinned version when Installed
	Dir       string // where it went
	NotOnPath bool   // that directory is not on PATH and was not added (Linux, macOS)
}

// EngramNeeded reports whether this plan uses Engram and GabitoTL has to
// install it: the memory choice is engram, none is on PATH (or the one GabitoTL
// installed is not on the user PATH yet) and a pinned release exists here.
// Env.Engram nil means the caller does not manage Engram.
func EngramNeeded(ctx context.Context, env Env, pl Plan) (engraminstall.Plan, bool) {
	if env.EngramSetup == nil || pl.MemoryChoice != gabitotl.MemoryEngram || !env.EngramSetup.Supported() {
		return engraminstall.Plan{}, false
	}
	p := env.EngramSetup.Plan(ctx)
	return p, p.Action == engraminstall.ActionInstall || len(p.Steps) > 0
}

// EnsureEngram installs Engram when EngramNeeded says so, reporting each step
// id, and then rebuilds the inventory and the plan so the agents also get
// Engram's own server entry. On failure the original inventory and plan come
// back with the error: the rest of the install does not depend on the binary.
func EnsureEngram(ctx context.Context, env Env, inv Inventory, pl Plan, step func(id string)) (Inventory, Plan, EngramOutcome, error) {
	if _, need := EngramNeeded(ctx, env, pl); !need {
		return inv, pl, EngramOutcome{}, nil
	}
	res, err := env.EngramSetup.Install(ctx, step)
	out := EngramOutcome{Dir: env.EngramSetup.Dir, NotOnPath: res.NotOnPath}
	if err != nil {
		return inv, pl, out, err
	}
	out.Installed, out.Version = res.Action == engraminstall.ActionInstall, res.Version
	inv2 := BuildInventory(ctx, env, nil)
	pl2, err := BuildPlan(ctx, env, inv2, pl.Options)
	if err != nil {
		return inv, pl, out, err
	}
	return inv2, pl2, out, nil
}
