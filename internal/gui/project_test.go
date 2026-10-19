package gui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

func TestProjectWithoutGabitotlReadsGitAndCredential(t *testing.T) {
	c := newCredRig(t)
	ctx := context.Background()
	d, err := c.svc.Project(ctx, c.root)
	if err != nil {
		t.Fatal(err)
	}
	if d.HasGabitoTL || !d.IsGit || d.Platform != "github" || d.Host != "github.com" || d.Name != filepath.Base(c.root) {
		t.Fatalf("detail %+v", d)
	}
	if d.Delivery.Source != SourceDefault || !d.Credential.Known || d.Credential.Host != "github.com" || d.Credential.Stored {
		t.Fatalf("delivery/credential %+v %+v", d.Delivery, d.Credential)
	}
	if len(d.Index) != 2 || !d.Index[0].Installed {
		t.Fatalf("the code index works without .gabitotl: %+v", d.Index)
	}
	_ = c.store.Set("github.com", "tok")
	if d, _ = c.svc.Project(ctx, c.root); !d.Credential.Stored {
		t.Fatal("a stored token must show as stored")
	}
	if _, err := c.svc.Project(ctx, ""); err == nil {
		t.Fatal("an empty root must be refused")
	}
}

func TestProjectDeliveryProvenanceAndReset(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.project()
	if d, _ := r.svc.Project(ctx, r.root); d.Delivery.Source != SourceDefault || !d.HasGabitoTL {
		t.Fatalf("nothing saved: %+v", d.Delivery)
	}
	if _, err := r.svc.SetGlobalDelivery(GlobalDeliveryAnswers{Automation: "ask", MergeMethod: "rebase"}); err != nil {
		t.Fatal(err)
	}
	if d, _ := r.svc.Project(ctx, r.root); d.Delivery.Source != SourceGlobal || !d.Delivery.Global.Saved {
		t.Fatalf("global saved: %+v", d.Delivery)
	}
	yes := true
	if _, err := r.svc.SetDelivery(r.root, DeliveryAnswers{Automation: "all", MergeMethod: "merge", Draft: &yes}); err != nil {
		t.Fatal(err)
	}
	d, _ := r.svc.Project(ctx, r.root)
	if d.Delivery.Source != SourceProject || d.Delivery.Automation != "all" || d.Delivery.MergeMethod != "merge" || !d.Delivery.Draft {
		t.Fatalf("override: %+v", d.Delivery)
	}
	if _, err := r.svc.ResetProjectDelivery(r.root); err != nil {
		t.Fatal(err)
	}
	d, _ = r.svc.Project(ctx, r.root)
	if d.Delivery.Source != SourceGlobal || d.Delivery.Automation != "ask" || d.Delivery.MergeMethod != "rebase" {
		t.Fatalf("after reset the project follows the global defaults: %+v", d.Delivery)
	}
	if _, err := r.svc.ResetProjectDelivery(t.TempDir()); err == nil {
		t.Fatal("reset needs a GabitoTL project")
	}
}

func TestProjectSetupIsPlanFirstAndSeedsGlobalDefaults(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	var remembered string
	r.svc.d.RememberProject = func(p string) error { remembered = p; return nil }
	if _, err := r.svc.SetGlobalDelivery(GlobalDeliveryAnswers{Automation: "ask", MergeMethod: "rebase"}); err != nil {
		t.Fatal(err)
	}
	pl, err := r.svc.PlanProjectSetup(ctx, r.root)
	if err != nil || !pl.Mutates || len(pl.Groups) != 1 || len(pl.Groups[0].Lines) < 5 {
		t.Fatalf("plan %+v %v", pl, err)
	}
	if !strings.Contains(strings.Join(pl.Groups[0].Lines, " "), ".gabitotl/vcs.md") {
		t.Fatalf("the plan must list the files: %v", pl.Groups[0].Lines)
	}
	if gabitotl.Exists(r.root) {
		t.Fatal("planning must not create anything")
	}
	if _, err := r.svc.ApplyProjectSetup(ctx, r.root, "", nil); !errors.Is(err, ErrPlanRequired) {
		t.Fatalf("no plan id: %v", err)
	}
	if _, err := r.svc.ApplyProjectSetup(ctx, r.root, "stale", nil); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("stale plan id: %v", err)
	}
	res, err := r.svc.ApplyProjectSetup(ctx, r.root, pl.ID, nil)
	if err != nil || len(res.Files) == 0 || !gabitotl.Exists(r.root) || remembered != r.root {
		t.Fatalf("apply %+v %v remembered=%q", res, err, remembered)
	}
	d, _ := r.svc.Project(ctx, r.root)
	if d.Delivery.Source != SourceGlobal || d.Delivery.MergeMethod != "rebase" || d.Delivery.Automation != "ask" {
		t.Fatalf("a new project must start from the global defaults: %+v", d.Delivery)
	}
	again, _ := r.svc.PlanProjectSetup(ctx, r.root)
	if again.Mutates || len(again.Notes) != 1 {
		t.Fatalf("second plan is a no-op: %+v", again)
	}
	if _, err := r.svc.PlanProjectSetup(ctx, ""); err == nil {
		t.Fatal("an empty root must be refused")
	}
}

func TestIndexProjectReportsProgressAndErrors(t *testing.T) {
	r := newRig(t)
	var steps []string
	p := func(e Event) { steps = append(steps, e.Status) }
	if err := r.svc.IndexProject(context.Background(), r.root, "codegraph", p); err != nil {
		t.Fatal(err)
	}
	if len(r.index.setups) != 1 || r.index.setups[0] != "codegraph" || strings.Join(steps, ",") != "running,done" {
		t.Fatalf("setups=%v steps=%v", r.index.setups, steps)
	}
	steps = nil
	if err := r.svc.IndexProject(context.Background(), r.root, "bogus", p); err == nil || strings.Join(steps, ",") != "running,error" {
		t.Fatalf("unknown tool: %v %v", err, steps)
	}
}
