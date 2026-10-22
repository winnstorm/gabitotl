package installer

import (
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// ErrUnknownSkill is returned for a name that is not an optional skill.
var ErrUnknownSkill = fmt.Errorf("not an optional skill (known: %s)", strings.Join(OptionalSkills(), ", "))

// SkillChange is what turning the optional skills on or off does in one agent:
// only the agents GabitoTL already manages are touched.
type SkillChange struct {
	Runtime string
	Label   string
	Dirs    []string    // the agent's skills directories
	Actions []rt.Action // only the skill files; CREATE/MODIFY/REMOVE-AFTER-VERIFY/NO-ACTION
}

// Pending reports whether the change writes or removes anything.
func (c SkillChange) Pending() bool {
	for _, a := range c.Actions {
		if a.Kind == rt.Create || a.Kind == rt.Modify || a.Kind == rt.RemoveAfterVerify {
			return true
		}
	}
	return false
}

// isOptionalSkillFile tells whether path lies in the directory of an optional skill.
func isOptionalSkillFile(path string, dirs []string) bool {
	for _, n := range OptionalSkills() {
		if skillFileOf(path, dirs, n) {
			return true
		}
	}
	return false
}

// skillFileOf tells whether path lies inside <dir>/<name>/ for one of the
// agent skills directories. Only the real directory prefix counts, so a project
// path that merely contains a /skills/<name>/ segment is never matched.
func skillFileOf(path string, dirs []string, name string) bool {
	p := filepath.Clean(path)
	for _, d := range dirs {
		prefix := filepath.Clean(filepath.Join(d, name)) + string(filepath.Separator)
		if len(p) < len(prefix) {
			continue
		}
		head := p[:len(prefix)]
		if head == prefix || (runtime.GOOS == "windows" && strings.EqualFold(head, prefix)) {
			return true
		}
	}
	return false
}

// agentSkillsDirs lists the skills directories (global and project scope) of an adapter.
func agentSkillsDirs(renv rt.Env, a rt.Adapter) []string {
	sd, ok := a.(rt.SkillsDirer)
	if !ok {
		return nil
	}
	return []string{sd.SkillsDir(renv, rt.ScopeGlobal), sd.SkillsDir(renv, rt.ScopeProject)}
}

// PlanSkillSelection plans the optional skills for the given selection in every
// managed agent. It writes nothing.
func PlanSkillSelection(env Env, selected []string) ([]SkillChange, error) {
	for _, n := range selected {
		if !slices.Contains(OptionalSkills(), n) {
			return nil, fmt.Errorf("%q: %w", n, ErrUnknownSkill)
		}
	}
	renv := env.RT()
	st, err := renv.Store().Load()
	if err != nil {
		return nil, err
	}
	var out []SkillChange
	for _, a := range env.Runtimes.All() {
		if len(st.ForRuntime(a.Name())) == 0 {
			continue
		}
		opts := RuntimeOptions(renv, "", rt.ScopeGlobal, "", false)
		opts.Skills, opts.RetireSkills = SkillSet(selected)
		acts, err := a.Plan(renv, opts)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", a.Name(), err)
		}
		c := SkillChange{Runtime: a.Name(), Label: a.Label(), Dirs: agentSkillsDirs(renv, a)}
		for _, ac := range acts {
			if isOptionalSkillFile(ac.Path, c.Dirs) {
				c.Actions = append(c.Actions, ac)
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// ApplySkillSelection applies PlanSkillSelection (backup first, per agent) and
// remembers the selection. It returns the files written and removed.
func ApplySkillSelection(env Env, selected []string) (written, removed []string, err error) {
	changes, err := PlanSkillSelection(env, selected)
	if err != nil {
		return nil, nil, err
	}
	renv := env.RT()
	for _, c := range changes {
		if !c.Pending() {
			continue
		}
		a, _ := env.Runtimes.Get(c.Runtime)
		_, res, err := rt.BackupAndApply(renv, a, c.Actions)
		for _, ac := range res.Applied {
			written = append(written, ac.Path)
		}
		for _, ac := range res.Removed {
			removed = append(removed, ac.Path)
		}
		if err != nil {
			return written, removed, fmt.Errorf("%s: %w", c.Runtime, err)
		}
	}
	_, err = gabitotl.SaveOptionalSkills(env.Home, SelectedSkills(selected))
	return written, removed, err
}

// SkillAgentState is one agent's view of an optional skill.
type SkillAgentState struct {
	Runtime   string
	Label     string
	Installed bool // every file of the skill is in place
}

// OptionalSkillState is an optional skill as the Manager, the CLI and the doctor show it.
type OptionalSkillState struct {
	Name    string
	Enabled bool // chosen by the person (saved selection)
	Agents  []SkillAgentState
}

// OptionalSkillStates reports every optional skill against the managed agents.
func OptionalSkillStates(env Env) ([]OptionalSkillState, error) {
	sel := gabitotl.LoadOptionalSkills(env.Home)
	changes, err := PlanSkillSelection(env, sel)
	if err != nil {
		return nil, err
	}
	var out []OptionalSkillState
	for _, name := range OptionalSkills() {
		s := OptionalSkillState{Name: name, Enabled: slices.Contains(sel, name)}
		for _, c := range changes {
			ag := SkillAgentState{Runtime: c.Runtime, Label: c.Label}
			files, settled, leftovers := 0, 0, 0
			for _, a := range c.Actions {
				if !skillFileOf(a.Path, c.Dirs, name) {
					continue
				}
				files++
				switch a.Kind {
				case rt.NoAction:
					settled++
				case rt.RemoveAfterVerify:
					leftovers++
				}
			}
			// Enabled: every file must be in place. Disabled: installed only while
			// files of ours are still there (a change is pending).
			ag.Installed = files > 0 && settled == files
			if !s.Enabled {
				ag.Installed = leftovers > 0
			}
			s.Agents = append(s.Agents, ag)
		}
		out = append(out, s)
	}
	return out, nil
}
