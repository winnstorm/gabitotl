package gui

import (
	"context"
	"fmt"
	"slices"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer"
)

// SkillAgent is one agent's copy of an optional skill.
type SkillAgent struct {
	Runtime   string `json:"runtime"`
	Label     string `json:"label"`
	Installed bool   `json:"installed"`
}

// SkillInfo is an optional skill as the Manager shows it.
type SkillInfo struct {
	Name    string       `json:"name"`
	Enabled bool         `json:"enabled"` // chosen by the person
	Agents  []SkillAgent `json:"agents"`  // the agents GabitoTL manages
}

// SkillRequest turns one optional skill on or off.
type SkillRequest struct {
	Name   string `json:"name"`
	Enable bool   `json:"enable"`
}

// Skills lists the optional skills with their state in each managed agent.
func (s *Service) Skills(_ context.Context) ([]SkillInfo, error) {
	env, err := s.env("")
	if err != nil {
		return nil, err
	}
	states, err := installer.OptionalSkillStates(env)
	if err != nil {
		return nil, err
	}
	out := []SkillInfo{}
	for _, st := range states {
		si := SkillInfo{Name: st.Name, Enabled: st.Enabled, Agents: []SkillAgent{}}
		for _, a := range st.Agents {
			si.Agents = append(si.Agents, SkillAgent{Runtime: a.Runtime, Label: a.Label, Installed: a.Installed})
		}
		out = append(out, si)
	}
	return out, nil
}

// selectionFor returns the saved selection with name turned on or off.
func selectionFor(home string, req SkillRequest) ([]string, error) {
	if !slices.Contains(installer.OptionalSkills(), req.Name) {
		return nil, fmt.Errorf("%w: %q is not an optional skill", ErrNotAvailable, req.Name)
	}
	sel := gabitotl.LoadOptionalSkills(home)
	if req.Enable && !slices.Contains(sel, req.Name) {
		sel = append(sel, req.Name)
	}
	if !req.Enable {
		sel = slices.DeleteFunc(slices.Clone(sel), func(n string) bool { return n == req.Name })
	}
	return sel, nil
}

// PlanSkill shows the files turning an optional skill on or off writes or removes
// in the agents GabitoTL manages. It writes nothing.
func (s *Service) PlanSkill(_ context.Context, req SkillRequest) (Plan, error) {
	env, err := s.env("")
	if err != nil {
		return Plan{}, err
	}
	sel, err := selectionFor(env.Home, req)
	if err != nil {
		return Plan{}, err
	}
	changes, err := installer.PlanSkillSelection(env, sel)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Groups: []PlanGroup{}, Notes: []Note{}, Steps: []PlanStep{}, Supported: true}
	m := msgs(env, "")
	for _, c := range changes {
		p.Groups = append(p.Groups, actionGroups(c.Actions)...)
		p.Mutates = p.Mutates || c.Pending()
	}
	if len(changes) == 0 {
		p.Notes = append(p.Notes, Note{Code: "skill-no-agent", Text: m.Format("gui.skills.noagent")})
	}
	// The choice itself is kept even when no agent changes (the next install uses it).
	if !slices.Equal(gabitotl.LoadOptionalSkills(env.Home), sel) {
		p.Mutates = true
	}
	return seal(p), nil
}

// ApplySkill runs the plan PlanSkill returned: backup first, then the files, then
// the saved choice.
func (s *Service) ApplySkill(ctx context.Context, req SkillRequest, planID string, p Progress) (SkillResult, error) {
	fresh, err := s.PlanSkill(ctx, req)
	if err != nil {
		return SkillResult{}, err
	}
	if err := checkPlan(planID, fresh); err != nil {
		return SkillResult{}, err
	}
	env, err := s.env("")
	if err != nil {
		return SkillResult{}, err
	}
	sel, err := selectionFor(env.Home, req)
	if err != nil {
		return SkillResult{}, err
	}
	p.emit("skill."+req.Name, "running", req.Name, 10)
	written, removed, err := installer.ApplySkillSelection(env, sel)
	p.emit("skill."+req.Name, statusOf(err), req.Name, 100)
	return SkillResult{Name: req.Name, Enabled: req.Enable, Written: nonNil(written), Removed: nonNil(removed)}, err
}

// SkillResult is what an apply did.
type SkillResult struct {
	Name    string   `json:"name"`
	Enabled bool     `json:"enabled"`
	Written []string `json:"written"`
	Removed []string `json:"removed"`
}

func nonNil(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}
