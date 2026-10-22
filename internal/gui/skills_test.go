package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func designPath(home string) string {
	return filepath.Join(home, ".claude", "skills", "gabito-design", "SKILL.md")
}

func TestWizardInstallsAnOptionalSkillOnlyWhenChosen(t *testing.T) {
	r := newRig(t)
	r.install(r.answers())
	if _, err := os.Stat(designPath(r.home)); err == nil {
		t.Fatal("GabitoDesign must be off by default")
	}
	a := r.answers()
	a.Skills = []string{"gabito-design"}
	pl, err := r.svc.PlanInstall(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range pl.Groups {
		for _, l := range g.Lines {
			found = found || strings.Contains(l, "gabito-design")
		}
	}
	if !found {
		t.Fatalf("the plan must list the GabitoDesign files: %+v", pl.Groups)
	}
	r.install(a)
	if _, err := os.Stat(designPath(r.home)); err != nil {
		t.Fatalf("selected skill not installed: %v", err)
	}
	a.Skills = nil
	r.install(a)
	if _, err := os.Stat(designPath(r.home)); err == nil {
		t.Fatal("deselecting in the wizard must remove it")
	}
	a.Skills = []string{"nope"}
	if _, err := r.svc.PlanInstall(context.Background(), a); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("unknown skill: %v", err)
	}
}

func TestManagerTogglesAnOptionalSkillPlanFirst(t *testing.T) {
	r := newRig(t)
	r.install(r.answers())
	ctx := context.Background()
	list, err := r.svc.Skills(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "gabito-design" || list[0].Enabled {
		t.Fatalf("skills %+v %v", list, err)
	}
	req := SkillRequest{Name: "gabito-design", Enable: true}
	if _, err := r.svc.ApplySkill(ctx, req, "", nil); !errors.Is(err, ErrPlanRequired) {
		t.Fatalf("apply without a plan: %v", err)
	}
	pl, err := r.svc.PlanSkill(ctx, req)
	if err != nil || !pl.Mutates || len(pl.Groups) == 0 {
		t.Fatalf("plan %+v %v", pl, err)
	}
	if _, err := os.Stat(designPath(r.home)); err == nil {
		t.Fatal("planning must not write")
	}
	if _, err := r.svc.ApplySkill(ctx, req, "stale", nil); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("stale plan: %v", err)
	}
	res, err := r.svc.ApplySkill(ctx, req, pl.ID, nil)
	if err != nil || len(res.Written) == 0 {
		t.Fatalf("apply %+v %v", res, err)
	}
	list, _ = r.svc.Skills(ctx)
	if !list[0].Enabled || len(list[0].Agents) == 0 || !list[0].Agents[0].Installed {
		t.Fatalf("after enabling: %+v", list)
	}
	off := SkillRequest{Name: "gabito-design"}
	pl, _ = r.svc.PlanSkill(ctx, off)
	if res, err = r.svc.ApplySkill(ctx, off, pl.ID, nil); err != nil || len(res.Removed) == 0 {
		t.Fatalf("disable %+v %v", res, err)
	}
	if _, err := os.Stat(designPath(r.home)); err == nil {
		t.Fatal("disabling must remove the files")
	}
	if _, err := r.svc.PlanSkill(ctx, SkillRequest{Name: "nope"}); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("unknown skill: %v", err)
	}
}
