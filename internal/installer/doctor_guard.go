package installer

import (
	"context"
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/guard"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// Authorship section line codes (presenters own the wording).
const (
	DocGuardMode          = "doctor.guard.mode"
	DocGuardHooksOK       = "doctor.guard.hooks-ok"
	DocGuardHooksMissing  = "doctor.guard.hooks-missing"
	DocGuardHooksDeclined = "doctor.guard.hooks-declined"
	DocGuardRuntimeOK     = "doctor.guard.runtime-ok"
	DocGuardRuntimeTodo   = "doctor.guard.runtime-todo"
	DocGuardRuntimeCustom = "doctor.guard.runtime-custom"
	DocGuardRuntimeNone   = "doctor.guard.runtime-none"
	DocGuardScanClean     = "doctor.guard.scan-clean"
	DocGuardScanFound     = "doctor.guard.scan-found"
	DocGuardHistory       = "doctor.guard.history"
)

// gitRunner adapts the installer Runner to the one package guard uses.
type gitRunner struct{ r Runner }

func (g gitRunner) Run(ctx context.Context, dir, name string, args ...string) (string, error) {
	out, _, err := g.r.Run(ctx, dir, name, args...)
	return strings.TrimSpace(string(out)), err
}

// guardDoctor is the read-only "Autoría/branding" section: guard mode, Git
// hooks, each runtime's own attribution setting and a scan summary. It never
// writes: Plan-free reads and `git` queries only.
func guardDoctor(ctx context.Context, env Env, hasProject bool, inv Inventory) []Line {
	var out []Line
	if hasProject {
		l := gabitotl.NewLayout(env.ProjectRoot)
		wf, err := gabitotl.LoadWorkflow(l)
		if errors.Is(err, fs.ErrNotExist) {
			wf, err = gabitotl.DefaultWorkflow(), nil
		}
		if err == nil {
			out = append(out, Line{LineOK, DocGuardMode, wf.BrandingGuard})
			out = append(out, guardGit(ctx, env, wf)...)
		}
	}
	renv := env.RT()
	for _, r := range inv.Runtimes {
		if r.State == rt.StateNone {
			continue
		}
		a, ok := env.Runtimes.Get(r.Name)
		rep, isRep := a.(rt.AttributionReporter)
		if !ok || !isRep {
			out = append(out, Line{LineOK, DocGuardRuntimeNone, r.Label})
			continue
		}
		switch st := rep.Attribution(renv, rt.ScopeGlobal); {
		case st.Hidden:
			out = append(out, Line{LineOK, DocGuardRuntimeOK, r.Label})
		case st.Custom:
			out = append(out, Line{LineWarn, DocGuardRuntimeCustom, r.Label + ": " + st.Detail})
		default:
			out = append(out, Line{LineTodo, DocGuardRuntimeTodo, r.Label})
		}
	}
	return out
}

func guardGit(ctx context.Context, env Env, wf gabitotl.WorkflowConfig) []Line {
	if env.Runner == nil {
		return nil
	}
	run := gitRunner{env.Runner}
	gctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var out []Line
	if dir, err := guard.HooksDir(gctx, run, env.ProjectRoot); err == nil {
		st := guard.HookStatus(dir)
		switch {
		case guard.AllInstalled(st):
			chained := 0
			for _, h := range st {
				if h.Chained {
					chained++
				}
			}
			out = append(out, Line{LineOK, DocGuardHooksOK, dir + " (" + strconv.Itoa(chained) + " chained)"})
		case wf.GuardHooks == gabitotl.HooksDeclined:
			out = append(out, Line{LineOK, DocGuardHooksDeclined, ""})
		default:
			out = append(out, Line{LineWarn, DocGuardHooksMissing, ""})
		}
	}
	s := guard.Settings{Mode: wf.BrandingGuard, ExtraTerms: wf.BrandingExtraTerms, AllowPaths: wf.BrandingAllowPaths}
	if found, err := guard.ScanAll(gctx, run, env.ProjectRoot, s); err == nil {
		if len(found) == 0 {
			out = append(out, Line{LineOK, DocGuardScanClean, ""})
		} else {
			out = append(out, Line{LineWarn, DocGuardScanFound, strconv.Itoa(len(found))})
		}
	}
	if hist, err := guard.ScanHistory(gctx, run, env.ProjectRoot, s); err == nil && len(hist) > 0 {
		seen := map[string]bool{}
		for _, h := range hist {
			seen[h.Commit] = true
		}
		out = append(out, Line{LineWarn, DocGuardHistory, strconv.Itoa(len(seen))})
	}
	return out
}
