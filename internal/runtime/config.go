package runtime

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// Resolved is the effective runtime selection for a project.
type Resolved struct {
	Runtime          string `json:"runtime"`
	Model            string `json:"model,omitempty"`
	AllowModelSwitch bool   `json:"allow_model_switch"`
	RuntimeSource    string `json:"runtime_source"` // "project", "local" or "" when unset
	ModelSource      string `json:"model_source,omitempty"`
}

// localRuntime is the front matter of .gabitotl/local/users/<id>/runtime.md: a
// per-person, per-worktree override. Only the fields that are set override the project file.
type localRuntime struct {
	Runtime string `yaml:"runtime,omitempty"`
	Model   string `yaml:"model,omitempty"`
}

const bodyLocal = `
# Local runtime override

Per-developer and per-worktree override of the project runtime and model.
This file is never committed.
`

// loadLocal reads the override of user. When that file is absent it falls back
// to the legacy shared local/runtime.md (read only, never migrated). An empty
// user has no override.
func loadLocal(l gabitotl.Layout, user string) (localRuntime, error) {
	var c localRuntime
	if user == "" {
		return c, nil
	}
	path, err := l.LocalRuntime(user)
	if err != nil {
		return c, err
	}
	d, err := gabitotl.ReadDoc(path)
	if errors.Is(err, fs.ErrNotExist) {
		d, err = gabitotl.ReadDoc(l.LegacyLocalRuntime())
		if errors.Is(err, fs.ErrNotExist) {
			return c, nil
		}
	}
	if err != nil {
		return c, err
	}
	return c, d.Decode(&c)
}

func loadProject(l gabitotl.Layout) (gabitotl.RuntimeConfig, error) {
	c, err := gabitotl.LoadRuntime(l)
	if errors.Is(err, fs.ErrNotExist) {
		return gabitotl.RuntimeConfig{Runtime: "unset", AllowModelSwitch: true}, nil
	}
	return c, err
}

// ResolveRuntime merges the project default (.gabitotl/runtime.md) with the
// override of user (.gabitotl/local/users/<user>/runtime.md); the local file
// wins. An empty user skips the override.
func ResolveRuntime(l gabitotl.Layout, user string) (Resolved, error) {
	p, err := loadProject(l)
	if err != nil {
		return Resolved{}, err
	}
	loc, err := loadLocal(l, user)
	if err != nil {
		return Resolved{}, err
	}
	r := Resolved{Runtime: p.Runtime, Model: p.Model, AllowModelSwitch: p.AllowModelSwitch}
	if p.Runtime != "" && p.Runtime != "unset" {
		r.RuntimeSource = "project"
	}
	if p.Model != "" {
		r.ModelSource = "project"
	}
	if loc.Runtime != "" {
		r.Runtime, r.RuntimeSource = loc.Runtime, "local"
	}
	if loc.Model != "" {
		r.Model, r.ModelSource = loc.Model, "local"
	}
	return r, nil
}

// SetRuntime stores the runtime and/or model (empty means unchanged) in the
// project default or, with local, in the override of user. It touches
// runtime.md only: persona, memory and workflow files are never written.
func SetRuntime(l gabitotl.Layout, user, runtimeName, model string, local bool) error {
	if runtimeName == "" && model == "" {
		return errors.New("runtime: nothing to set (give a runtime and/or a model)")
	}
	p, err := loadProject(l)
	if err != nil {
		return err
	}
	if model != "" && !p.AllowModelSwitch {
		return fmt.Errorf("runtime: runtime.md has allow_model_switch: false; refusing to change the model")
	}
	if local {
		path, err := l.LocalRuntime(user)
		if err != nil {
			return fmt.Errorf("runtime: a local override needs the active user: %w", err)
		}
		loc, err := loadLocal(l, user)
		if err != nil {
			return err
		}
		if runtimeName != "" {
			loc.Runtime = runtimeName
		}
		if model != "" {
			loc.Model = model
		}
		body := bodyLocal
		if d, err := gabitotl.ReadDoc(path); err == nil {
			body = d.Body
		}
		if _, err := gabitotl.EnsureLocalUserDir(l, user); err != nil {
			return err
		}
		return gabitotl.WriteDoc(path, loc, body)
	}
	if runtimeName != "" {
		p.Runtime = runtimeName
	}
	if model != "" {
		p.Model = model
	}
	return gabitotl.SaveRuntime(l, p)
}
