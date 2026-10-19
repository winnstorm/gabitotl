package credstore

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func exercise(t *testing.T, s Store) {
	t.Helper()
	if _, ok, err := s.Get("GitHub.com"); ok || err != nil {
		t.Fatalf("empty store: ok=%v err=%v", ok, err)
	}
	if err := s.Set("GitHub.com", " tok-1 "); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("gitlab.example.org:8443", "tok-2"); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := s.Get("github.com"); !ok || v != "tok-1" {
		t.Fatalf("get = %q %v", v, ok)
	}
	if err := s.Set("github.com", "tok-3"); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := s.Get("github.com"); v != "tok-3" {
		t.Fatalf("replace = %q", v)
	}
	if v, ok, _ := s.Get("gitlab.example.org:8443"); !ok || v != "tok-2" {
		t.Fatalf("other host lost: %q", v)
	}
	if err := s.Delete("github.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("github.com"); err != nil {
		t.Fatalf("deleting a missing token must not fail: %v", err)
	}
	if _, ok, _ := s.Get("github.com"); ok {
		t.Fatal("still stored after delete")
	}
}

func TestFake(t *testing.T) { exercise(t, NewFake()) }

func TestFile(t *testing.T) {
	home := t.TempDir()
	f := NewFile(home)
	exercise(t, f)
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(f.Path)
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("file mode = %v err=%v", fi, err)
		}
		di, _ := os.Stat(home + "/.gabitotl/secrets")
		if di.Mode().Perm() != 0o700 {
			t.Fatalf("dir mode = %v", di.Mode().Perm())
		}
	}
}

func TestRejectsBadInput(t *testing.T) {
	for _, s := range []Store{NewFake(), NewFile(t.TempDir())} {
		for _, h := range []string{"", "a b", "../x", "x=y"} {
			if s.Set(h, "t") == nil {
				t.Fatalf("host %q accepted", h)
			}
		}
		for _, tok := range []string{"", "  ", "a\nb"} {
			if s.Set("github.com", tok) == nil {
				t.Fatalf("token %q accepted", tok)
			}
		}
	}
}

func TestTarget(t *testing.T) {
	if got := Target(" GitHub.com "); got != "gabitotl:vcs:github.com" {
		t.Fatal(got)
	}
	if !strings.HasPrefix(TargetPrefix, "gabitotl:") {
		t.Fatal()
	}
}
