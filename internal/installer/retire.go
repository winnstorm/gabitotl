package installer

import (
	"context"
	"errors"
	"fmt"

	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// ErrRetireFailed means Gentle-AI's own uninstall failed or left GabitoTL
// unverified. By then GabitoTL itself was verified, so nothing is rolled back:
// the snapshot stays available through `gabitotl restore`.
var ErrRetireFailed = errors.New("retiring Gentle-AI did not complete")

// PartialError marks a failure after a destructive step (Gentle-AI's own
// uninstall). Reverting GabitoTL would not bring Gentle-AI back, so Run keeps
// the verified GabitoTL setup and reports the snapshot to restore from.
type PartialError struct {
	Phase Phase
	Err   error
}

func (e *PartialError) Error() string { return fmt.Sprintf("%s: %v", e.Phase, e.Err) }
func (e *PartialError) Unwrap() error { return e.Err }

// retire is the only place anything of Gentle-AI or a foreign persona is
// removed. The gate below makes invariant (b) a code path, not a convention:
// without a clean set of critical checks nothing is retired.
func (r *run) retire(ctx context.Context) error {
	g := r.inv.Other.GentleAI
	args, manual := GentleUninstallArgs(g, r.pl.RuntimeOrder)
	if manual {
		r.warn("Gentle-AI also manages agents GabitoTL does not replace: its `uninstall --all` was not run; run Gentle-AI's own uninstall manually")
	}
	personaWork := false
	for _, acts := range r.pl.Retire {
		personaWork = personaWork || len(acts) > 0
	}
	maybeGentle := g.Found && args != nil && (r.pl.Options.RemoveGentleAI || r.hooks.ConfirmRetire != nil)
	kiroWork := r.pl.retireGentleKiro()
	if !personaWork && !maybeGentle && !kiroWork {
		if r.pl.Options.RemoveGentleAI && g.Found && !manual {
			r.warn("no selected runtime matches a Gentle-AI agent: nothing was retired")
		}
		return nil // nothing to retire: no retire phase
	}
	// The gate: without a clean set of critical checks nothing is retired (b).
	if len(r.res.Checks) == 0 || len(Failed(r.res.Checks)) > 0 {
		return ErrNotVerified
	}
	r.phase(PhaseRetire)
	renv := r.env.RT()

	// 1. Managed persona blocks of another harness, through the adapter, only
	// with the user's consent (--migrate-persona or the interactive question).
	for _, name := range r.pl.RuntimeOrder {
		acts := r.pl.Retire[name]
		if len(acts) == 0 {
			continue
		}
		a, _ := r.env.Runtimes.Get(name)
		res, err := a.Apply(renv, acts)
		if err != nil {
			return fmt.Errorf("retire persona in %s: %w", name, err)
		}
		blocks := 0
		for _, rm := range res.Removed {
			if rm.Edit.Op == rt.OpRemoveKey { // a memory entry replaced by the new provider's, not a persona
				r.emit(PhaseRetire, "ok", "retire memory entry "+name, rm.Target)
				continue
			}
			r.res.Retired.Personas = append(r.res.Retired.Personas, rm)
			blocks++
		}
		if blocks > 0 {
			r.emit(PhaseRetire, "ok", "retire persona "+name, fmt.Sprintf("%d block(s)", blocks))
		}
	}

	// 1b. Gentle AI's files in Kiro: already in the snapshot, removed only now.
	if kiroWork {
		if err := r.retireGentleKiro(); err != nil {
			return err
		}
	}

	// 2. Gentle-AI itself, with its own uninstall mechanism, never by hand. The
	// presenter is asked only now, once everything is verified.
	remove := r.pl.Options.RemoveGentleAI
	if maybeGentle && !remove && r.hooks.ConfirmRetire != nil {
		remove = r.hooks.ConfirmRetire(RetirePreview{Args: args})
	}
	if !maybeGentle || !remove {
		return r.postVerify(ctx)
	}
	bin, ok := r.env.Look("gentle-ai")
	if !ok {
		return &PartialError{PhaseRetire, fmt.Errorf("%w: the gentle-ai binary was not found, so its own uninstall cannot run", ErrRetireFailed)}
	}
	if _, _, err := r.env.run(ctx, "", bin, args...); err != nil {
		return &PartialError{PhaseRetire, fmt.Errorf("%w: gentle-ai %v: %v", ErrRetireFailed, args, err)}
	}
	r.res.Retired.GentleArgs = args
	r.emit(PhaseRetire, "ok", "gentle-ai uninstall", fmt.Sprint(args))
	if err := r.postVerify(ctx); err != nil {
		return &PartialError{PhaseRetire, err}
	}
	return nil
}

// postVerify checks everything again after retirement. Gentle-AI's uninstall
// may remove an MCP entry both tools shared; if so, GabitoTL reconnects its own
// entries once and verifies again.
func (r *run) postVerify(ctx context.Context) error {
	check := func() []Check {
		cs := r.verifyMemory(ctx, "post-retire")
		cs = append(cs, r.verifyProject()...)
		return append(cs, r.verifyRuntimes("post-retire")...)
	}
	cs := check()
	if len(Failed(cs)) > 0 && len(r.res.Retired.GentleArgs) > 0 {
		r.emit(PhaseRetire, "warn", "reconnect", "Gentle-AI's uninstall removed shared entries")
		renv := r.env.RT()
		for _, name := range r.pl.RuntimeOrder {
			a, _ := r.env.Runtimes.Get(name)
			plan, err := a.Plan(renv, r.ropts())
			if err != nil {
				continue
			}
			var install []rt.Action
			for _, ac := range plan {
				if ac.Kind != rt.RemoveAfterVerify {
					install = append(install, ac)
				}
			}
			if _, err := a.Apply(renv, install); err != nil {
				r.warn("reconnect %s: %v", name, err)
			}
		}
		cs = check()
	}
	r.res.Checks = append(r.res.Checks, cs...)
	if bad := Failed(cs); len(bad) > 0 {
		return fmt.Errorf("%w: %s: %s", ErrVerifyFailed, bad[0].Name, bad[0].Detail)
	}
	return nil
}
