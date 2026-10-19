package installer

import (
	"path/filepath"
	"runtime"

	"github.com/winnstorm/gabitotl/internal/codegraph"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// CodeGraphTools returns the CodeGraph MCP entry the agents should have: when
// force is set (the caller installs CodeGraph in the same run) or, on Windows,
// when CodeGraph is already installed. Other systems install it themselves (npm)
// and keep whatever entry they have, so nothing is planned there.
func CodeGraphTools(renv rt.Env, force bool) []rt.MemoryMCP {
	goos := renv.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	if !force {
		if goos != "windows" {
			return nil
		}
		_, onPath := renv.Look("codegraph")
		local := renv.Var("LOCALAPPDATA")
		managed := local != "" && renv.Has(filepath.Join(local, "codegraph", "current", "bin", "codegraph.cmd"))
		if !onPath && !managed {
			return nil
		}
	}
	return []rt.MemoryMCP{codegraph.MCP(goos)}
}
