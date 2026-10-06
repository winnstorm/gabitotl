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

// localRuntime is the front matter of .gabitotl/local/runtime.md: a per-user,
// per-worktree override. Only the fields that are set override the project file.
type localRuntime struct {
	Runtime string `yaml:"runtime,omitempty"`
	Model   string `yaml:"model,omitempty"`
}

const bodyLocal = `
# Local runtime override

Per-developer and per-worktree override of the project runtime and model.
This file is never committed.
`

func loadLocal(l gabitotl.Layout) (localRuntime, error) {
	var c localRuntime
	d, err := gabitotl.ReadDoc(l.LocalRuntime())
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
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
// local override (.gabitotl/local/runtime.md); the local file wins.
func ResolveRuntime(l gabitotl.Layout) (Resolved, error) {
	p, err := loadProject(l)
	if err != nil {
		return Resolved{}, err
	}
	loc, err := loadLocal(l)
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
// project default or, with local, in the per-user override. It touches
// runtime.md only: persona, memory and workflow files are never written.
func SetRuntime(l gabitotl.Layout, runtimeName, model string, local bool) error {
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
		loc, err := loadLocal(l)
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
		if d, err := gabitotl.ReadDoc(l.LocalRuntime()); err == nil {
			body = d.Body
		}
		return gabitotl.WriteDoc(l.LocalRuntime(), loc, body)
	}
	if runtimeName != "" {
		p.Runtime = runtimeName
	}
	if model != "" {
		p.Model = model
	}
	return gabitotl.SaveRuntime(l, p)
}
