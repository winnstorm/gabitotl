// Package all is the composition root of the runtime adapters: the only place
// that imports the concrete adapter packages.
package all

import (
	rt "github.com/winnstorm/gabitotl/internal/runtime"
	"github.com/winnstorm/gabitotl/internal/runtime/claudecode"
	"github.com/winnstorm/gabitotl/internal/runtime/kiro"
	"github.com/winnstorm/gabitotl/internal/runtime/opencode"
)

// Registry returns a registry with every V1 adapter.
func Registry() *rt.Registry {
	r := &rt.Registry{}
	r.Register(claudecode.New())
	r.Register(opencode.New())
	r.Register(kiro.NewCLI())
	r.Register(kiro.NewIDE())
	return r
}
