//go:build !windows

package procattr

import "os/exec"

func noWindow(*exec.Cmd) {}
