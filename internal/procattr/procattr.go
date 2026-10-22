// Package procattr adjusts child processes for the host OS.
//
// On Windows a GUI process (the Manager) has no console, so every console
// child it starts (git, engram, codegraph, gh...) would get a new console
// window of its own. NoWindow stops that. Elsewhere, and in a process that
// already has a console (the CLI in a terminal), it does nothing, so children
// keep sharing the terminal as before.
package procattr

import "os/exec"

// NoWindow marks cmd so that it opens no console window. It returns cmd so it
// can wrap exec.Command directly. Call it before Start/Run/Output.
func NoWindow(cmd *exec.Cmd) *exec.Cmd {
	if cmd != nil {
		noWindow(cmd)
	}
	return cmd
}
