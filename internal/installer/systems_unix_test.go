//go:build !windows

package installer

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestMarkerScanSkipsNonRegularFiles(t *testing.T) {
	env := sysEnv(t, "linux", nil)
	p := filepath.Join(env.Home, ".claude/CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(p, 0o644); err != nil {
		t.Skip(err)
	}
	done := make(chan bool, 1)
	go func() { done <- fileHasMarker(p, openspecMarker) }()
	select {
	case got := <-done:
		if got {
			t.Fatal("a FIFO is never a hit")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("scan blocked on a FIFO")
	}
}
