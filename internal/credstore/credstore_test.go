package credstore

import (
	"errors"
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

func TestFileRefusesEmptyOrRelativeHome(t *testing.T) {
	wd := t.TempDir()
	t.Chdir(wd)
	for _, home := range []string{"", ".", "rel/home"} {
		if FilePath(home) != "" {
			t.Fatalf("FilePath(%q) must be empty", home)
		}
		f := NewFile(home)
		if err := f.Set("github.com", "t"); !errors.Is(err, ErrNoHome) {
			t.Fatalf("Set with home %q: %v", home, err)
		}
		if _, _, err := f.Get("github.com"); !errors.Is(err, ErrNoHome) {
			t.Fatalf("Get with home %q: %v", home, err)
		}
		if err := f.Delete("github.com"); !errors.Is(err, ErrNoHome) {
			t.Fatalf("Delete with home %q: %v", home, err)
		}
	}
	if entries, _ := os.ReadDir(wd); len(entries) != 0 {
		t.Fatalf("nothing may be written under the working directory: %v", entries)
	}
}

func TestPortIsNotPartOfTheKey(t *testing.T) {
	for _, s := range []Store{NewFake(), NewFile(t.TempDir())} {
		if err := s.Set("Git.Corp:8443", "tok"); err != nil {
			t.Fatal(err)
		}
		if v, ok, _ := s.Get("git.corp"); !ok || v != "tok" {
			t.Fatalf("lookup by bare host: %q %v", v, ok)
		}
		if _, ok, _ := s.Get("git.corp:22"); !ok {
			t.Fatal("any port maps to the same key")
		}
	}
	if Target("git.corp:8443") != "gabitotl:vcs:git.corp" || NormalizeHost("a:b") != "a:b" {
		t.Fatal(Target("git.corp:8443"))
	}
	if ValidHost("a:b") {
		t.Fatal("a non-numeric port suffix is not a host")
	}
}
