package procattr

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

var getConsoleWindow = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")

// hasConsole reports whether this process is attached to a console.
func hasConsole() bool {
	if getConsoleWindow.Find() != nil {
		return false
	}
	h, _, _ := getConsoleWindow.Call()
	return h != 0
}

func noWindow(cmd *exec.Cmd) {
	if hasConsole() {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
