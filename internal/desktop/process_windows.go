//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmClose            = 0x0010
	processTerminate   = 0x0001
	processSynchronize = 0x00100000
)

var (
	user32      = windows.NewLazySystemDLL("user32.dll")
	postMessage = user32.NewProc("PostMessageW")
	// enumTop is created once: Go caps the number of syscall callbacks.
	enumTop = windows.NewCallback(func(h windows.HWND, param uintptr) uintptr {
		c := enumCur
		var pid uint32
		_, _ = windows.GetWindowThreadProcessId(h, &pid)
		if pid != c.pid {
			return 1
		}
		if c.close {
			_, _, _ = postMessage.Call(uintptr(h), wmClose, 0, 0)
		}
		if windows.IsWindowVisible(h) {
			c.visible = true
		}
		return 1
	})
)

// enumCur is the context of the running enumeration; enumMu serializes them
// (the callback cannot carry a Go pointer through its uintptr parameter).
var (
	enumMu  sync.Mutex
	enumCur *enumCtx
)

func enumWindows(c *enumCtx) {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumCur = c
	_ = windows.EnumWindows(enumTop, nil)
	enumCur = nil
}

type enumCtx struct {
	pid     uint32
	close   bool
	visible bool
}

// winProcs lists and stops processes through the Win32 API (same user only).
type winProcs struct{}

func (winProcs) Running(dir string, exes []string) ([]Proc, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var out []Proc
	self := uint32(os.Getpid())
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if e.ProcessID == self {
			continue
		}
		path, started := imageOf(e.ProcessID)
		if path == "" || !Owned(path, dir, exes) {
			continue
		}
		c := enumCtx{pid: e.ProcessID}
		enumWindows(&c)
		out = append(out, Proc{PID: e.ProcessID, Path: path, Started: started, HasWindow: c.visible})
	}
	return out, nil
}

// imageOf returns the full image path and start time of pid, or "" when the
// process cannot be opened (another user, or already gone).
func imageOf(pid uint32) (string, time.Time) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", time.Time{}
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 32768)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return "", time.Time{}
	}
	var c, x, k, u windows.Filetime
	var t time.Time
	if windows.GetProcessTimes(h, &c, &x, &k, &u) == nil {
		t = time.Unix(0, c.Nanoseconds())
	}
	return windows.UTF16ToString(buf[:n]), t
}

// winHandle is the single handle of a Stop sequence.
type winHandle struct {
	h   windows.Handle
	pid uint32
}

func (w winHandle) Identity() (string, time.Time, error) {
	buf := make([]uint16, 32768)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(w.h, 0, &buf[0], &n); err != nil {
		return "", time.Time{}, err
	}
	var c, x, k, u windows.Filetime
	if err := windows.GetProcessTimes(w.h, &c, &x, &k, &u); err != nil {
		return "", time.Time{}, err
	}
	return windows.UTF16ToString(buf[:n]), time.Unix(0, c.Nanoseconds()), nil
}

func (w winHandle) CloseWindows() bool {
	c := enumCtx{pid: w.pid, close: true}
	enumWindows(&c)
	return c.visible
}

func (w winHandle) Wait(d time.Duration) bool {
	ev, _ := windows.WaitForSingleObject(w.h, uint32(d.Milliseconds()))
	return ev == windows.WAIT_OBJECT_0
}

func (w winHandle) Terminate() error { return windows.TerminateProcess(w.h, 1) }

func (winProcs) Stop(p Proc, grace time.Duration) error {
	h, err := windows.OpenProcess(processTerminate|processSynchronize|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, p.PID)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil // already gone
		}
		return fmt.Errorf("open: %w", err)
	}
	defer windows.CloseHandle(h)
	return stopProcess(p, grace, winHandle{h: h, pid: p.PID})
}
