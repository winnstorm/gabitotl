package codegraph

import rt "github.com/winnstorm/gabitotl/internal/runtime"

// MCP is the stdio server entry registered in the agents. On Windows the
// launcher is a codegraph.cmd batch file, which cannot be spawned directly as
// a stdio server, so it runs through cmd (the usual Windows form for Node-style
// CLIs); the bin directory is on the user PATH, which keeps the entry valid
// across upgrades. Elsewhere the binary is called directly.
func MCP(goos string) rt.MemoryMCP {
	if goos == "windows" {
		return rt.MemoryMCP{Name: rt.EntryCodeGraph, Command: "cmd", Args: []string{"/c", "codegraph", "serve", "--mcp"}}
	}
	return rt.MemoryMCP{Name: rt.EntryCodeGraph, Command: "codegraph", Args: []string{"serve", "--mcp"}}
}
