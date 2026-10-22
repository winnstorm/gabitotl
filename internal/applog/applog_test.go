package applog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotationKeepsTwoFiles(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "logs", FileName)
	l, err := Open(p, 300)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for i := 0; i < 60; i++ {
		l.Infof("line %d %s", i, strings.Repeat("x", 20))
	}
	ents, _ := os.ReadDir(filepath.Dir(p))
	if len(ents) != 2 {
		t.Fatalf("want current + 1 previous file, got %d", len(ents))
	}
	st, _ := os.Stat(p)
	if st.Size() > 300 {
		t.Fatalf("current file %d bytes passed the cap", st.Size())
	}
	if _, err := os.Stat(p + ".1"); err != nil {
		t.Fatal(err)
	}
}

func TestScrubMasksSecrets(t *testing.T) {
	in := "token=abc123 GITHUB_TOKEN: ghp_" + strings.Repeat("a", 30) + " Authorization: Bearer abcdefghijkl glpat-" + strings.Repeat("b", 20) + " path C:\\x"
	out := Scrub(in)
	for _, bad := range []string{"abc123", "ghp_aaaa", "abcdefghijkl", "glpat-bbbb"} {
		if strings.Contains(out, bad) {
			t.Fatalf("%q leaked in %q", bad, out)
		}
	}
	if !strings.Contains(out, `C:\x`) {
		t.Fatalf("plain text lost: %q", out)
	}
}

func TestLinesAreScrubbedOnWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	l, _ := Open(p, 0)
	l.Errorf("failed password=hunter2")
	l.Close()
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "hunter2") || !strings.Contains(string(b), "ERROR") {
		t.Fatalf("bad log: %q", b)
	}
}

func TestNilLoggerIsSafe(t *testing.T) {
	var l *Logger
	l.Infof("x")
	l.Panic("w", "v")
	l.Close()
	if l.Path() != "" {
		t.Fatal("nil path")
	}
}

func TestPanicHasStack(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	l, _ := Open(p, 0)
	l.Panic("boot", "boom")
	l.Close()
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "PANIC boot: boom") || !strings.Contains(string(b), "goroutine") {
		t.Fatalf("%q", b)
	}
}

func TestSafeArgsDropsValues(t *testing.T) {
	got := SafeArgs([]string{"--manage", `C:\Users\bob\x`, "--token=abc"})
	if strings.Contains(got, "bob") || strings.Contains(got, "abc") || !strings.Contains(got, "--manage") {
		t.Fatal(got)
	}
}
