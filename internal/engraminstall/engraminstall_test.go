package engraminstall

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/codegraph"
)

const fakeBin = "#!/bin/sh\necho engram 9.9.9\n"

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for n, c := range files {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(c))
	}
	_ = zw.Close()
	return b.Bytes()
}

func tgzOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for n, c := range files {
		_ = tw.WriteHeader(&tar.Header{Name: n, Mode: 0o755, Size: int64(len(c)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(c))
	}
	_ = tw.Close()
	_ = gz.Close()
	return b.Bytes()
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

type fakePath struct {
	val  string
	sets int
}

func (f *fakePath) Get() (string, error) { return f.val, nil }
func (f *fakePath) Set(v string) error   { f.val = v; f.sets++; return nil }

// serve serves one archive over TLS and returns an installer wired to it.
func serve(t *testing.T, goos, name string, body []byte, hash string) (*Installer, *int) {
	t.Helper()
	hits := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+name {
			http.NotFound(w, r)
			return
		}
		hits++
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	dir := filepath.Join(t.TempDir(), "bin")
	in := &Installer{
		Ports: Ports{
			Download: codegraph.NewHTTPDownloaderWith(srv.Client()),
			Look:     func(string) (string, error) { return "", errors.New("not found") },
			Run:      func(context.Context, string, ...string) (string, error) { return "", errors.New("no run") },
			Getenv:   func(string) string { return "" },
		},
		GOOS: goos, GOARCH: "amd64", Dir: dir, BaseURL: srv.URL + "/",
		Table: map[string]Asset{goos + "/amd64": {Name: name, SHA256: hash}},
		Token: func() string { return "tok" },
	}
	return in, &hits
}

func TestInstallLinuxTarGz(t *testing.T) {
	body := tgzOf(t, map[string]string{"engram": fakeBin, "tools/cloud-sync-projects.sh": "x"})
	in, _ := serve(t, "linux", "e.tar.gz", body, sum(body))
	var procs []string
	in.SetProcessPath = func(d string) { procs = append(procs, d) }
	var steps []string
	res, err := in.Install(context.Background(), func(id string) { steps = append(steps, id) })
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(in.Dir, "engram"))
	if err != nil || string(got) != fakeBin {
		t.Fatalf("binary = %q, %v", got, err)
	}
	if fi, _ := os.Stat(filepath.Join(in.Dir, "engram")); fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", fi.Mode())
	}
	if strings.Join(steps, ",") != "download,verify,extract,activate" || res.Action != ActionInstall || res.Version != Version {
		t.Fatalf("steps=%v res=%+v", steps, res)
	}
	if !res.NotOnPath {
		t.Fatal("~/.local/bin is not on PATH: the user must be told")
	}
	if len(procs) != 1 || procs[0] != in.Dir {
		t.Fatalf("process PATH not updated: %v", procs)
	}
	ents, _ := os.ReadDir(in.Dir)
	if len(ents) != 1 {
		t.Fatalf("stage files left behind: %v", ents)
	}
}

func TestInstallWindowsZipAddsUserPath(t *testing.T) {
	body := zipOf(t, map[string]string{"engram.exe": "MZ-fake", "tools/x.ps1": "x"})
	in, _ := serve(t, "windows", "e.zip", body, sum(body))
	fp := &fakePath{val: `C:\Windows`}
	in.Path = fp
	res, err := in.Install(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if fp.sets != 1 || !strings.Contains(fp.val, in.Dir) {
		t.Fatalf("user PATH = %q (sets %d)", fp.val, fp.sets)
	}
	if res.NotOnPath || filepath.Base(res.Path) != "engram.exe" {
		t.Fatalf("res = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(in.Dir, "engram.exe")); err != nil {
		t.Fatal(err)
	}
}

func TestHashMismatchLeavesNothing(t *testing.T) {
	body := tgzOf(t, map[string]string{"engram": fakeBin})
	in, _ := serve(t, "linux", "e.tar.gz", body, strings.Repeat("0", 64))
	_, err := in.Install(context.Background(), nil)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("err = %v", err)
	}
	if ents, _ := os.ReadDir(in.Dir); len(ents) != 0 {
		t.Fatalf("files left after a mismatch: %v", ents)
	}
}

func TestRejectsUnsafeArchives(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"dotdot":   {"../engram": "x"},
		"absolute": {"/etc/engram": "x"},
		"drive":    {"C:/engram": "x"},
		"device":   {"tools/NUL.txt": "x", "engram": "y"},
		"missing":  {"other": "x"},
	} {
		body := tgzOf(t, files)
		in, _ := serve(t, "linux", "e.tar.gz", body, sum(body))
		if _, err := in.Install(context.Background(), nil); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if _, err := os.Stat(filepath.Join(in.Dir, "engram")); err == nil {
			t.Errorf("%s: binary written", name)
		}
	}
	zb := zipOf(t, map[string]string{"..\\engram.exe": "x"})
	in, _ := serve(t, "windows", "e.zip", zb, sum(zb))
	if _, err := in.Install(context.Background(), nil); err == nil {
		t.Error("zip-slip accepted")
	}
}

func TestDownloadErrorAndOversize(t *testing.T) {
	in, _ := serve(t, "linux", "e.tar.gz", []byte("x"), "00")
	in.BaseURL += "missing/"
	if _, err := in.Install(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "download") {
		t.Fatalf("err = %v", err)
	}
}

func TestDetectsExistingEngramAndDoesNothing(t *testing.T) {
	body := tgzOf(t, map[string]string{"engram": fakeBin})
	in, hits := serve(t, "linux", "e.tar.gz", body, sum(body))
	in.Look = func(n string) (string, error) { return "/usr/bin/engram", nil }
	in.Run = func(_ context.Context, bin string, a ...string) (string, error) { return "engram 1.2.3\n", nil }
	st := in.Status(context.Background())
	if !st.Installed || st.Version != "1.2.3" || st.Managed {
		t.Fatalf("status = %+v", st)
	}
	if pl := in.Plan(context.Background()); pl.Action != ActionSkip || len(pl.Steps) != 0 {
		t.Fatalf("plan = %+v", pl)
	}
	res, err := in.Install(context.Background(), nil)
	if err != nil || res.Action != ActionSkip || res.Version != "1.2.3" || *hits != 0 {
		t.Fatalf("res=%+v err=%v hits=%d", res, err, *hits)
	}
	if _, err := os.Stat(in.Target()); err == nil {
		t.Fatal("a user-managed Engram was shadowed")
	}
}

func TestManagedCopyNotOnPathOnWindowsOnlyAddsPath(t *testing.T) {
	body := zipOf(t, map[string]string{"engram.exe": "MZ"})
	in, hits := serve(t, "windows", "e.zip", body, sum(body))
	in.Path = &fakePath{}
	_ = os.MkdirAll(in.Dir, 0o755)
	_ = os.WriteFile(in.Target(), []byte("old"), 0o755)
	pl := in.Plan(context.Background())
	if pl.Action != ActionSkip || len(pl.Steps) != 1 || pl.Steps[0] != StepPath {
		t.Fatalf("plan = %+v", pl)
	}
	if _, err := in.Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(in.Target()); string(b) != "old" || *hits != 0 {
		t.Fatal("an existing managed binary must not be replaced")
	}
	if !strings.Contains(in.Path.(*fakePath).val, in.Dir) {
		t.Fatal("PATH not added")
	}
}

func TestSwapRollsBackWhenActivationFails(t *testing.T) {
	in, _ := serve(t, "linux", "e", nil, "")
	_ = os.MkdirAll(in.Dir, 0o755)
	target := in.Target()
	stage := filepath.Join(in.Dir, ".new")
	_ = os.WriteFile(target, []byte("old"), 0o755)
	_ = os.WriteFile(stage, []byte("new"), 0o755)
	in.rename = func(a, b string) error {
		if a == stage {
			return errors.New("locked")
		}
		return os.Rename(a, b)
	}
	if err := in.swap(stage, target, "t"); err == nil {
		t.Fatal("swap should fail")
	}
	if b, _ := os.ReadFile(target); string(b) != "old" {
		t.Fatalf("previous install not restored: %q", b)
	}
	in.rename = nil
	if err := in.swap(stage, target, "t"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "new" {
		t.Fatalf("target = %q", b)
	}
	if _, err := os.Stat(target + ".old-t"); err == nil {
		t.Fatal("aside file left behind")
	}
}

func TestUnsupportedSystem(t *testing.T) {
	in := &Installer{GOOS: "plan9", GOARCH: "386", Dir: t.TempDir()}
	if pl := in.Plan(context.Background()); pl.Action != ActionUnsupported {
		t.Fatalf("plan = %+v", pl)
	}
	if _, err := in.Install(context.Background(), nil); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestPinnedAssetsAreWellFormed(t *testing.T) {
	for k, a := range Assets {
		if len(a.SHA256) != 64 || !strings.Contains(a.Name, Version) || !strings.Contains(a.Name, strings.ReplaceAll(k, "/", "_")) {
			t.Errorf("%s: %+v", k, a)
		}
	}
}

func TestDefaultDir(t *testing.T) {
	env := func(k string) string {
		if k == "LOCALAPPDATA" {
			return `C:\Users\a\AppData\Local`
		}
		return ""
	}
	home := func() (string, error) { return "/home/a", nil }
	if got := DefaultDir("windows", env, home); got != filepath.Join(`C:\Users\a\AppData\Local`, "Programs", "engram") {
		t.Fatal(got)
	}
	if got := DefaultDir("linux", env, home); got != filepath.Join("/home/a", ".local", "bin") {
		t.Fatal(got)
	}
	if DefaultDir("windows", func(string) string { return "" }, home) != "" {
		t.Fatal("an unresolved %LOCALAPPDATA% must give no directory")
	}
}

func TestFailedDownloadRemovesTheDirItCreated(t *testing.T) {
	in, _ := serve(t, "linux", "e.tar.gz", []byte("x"), sum([]byte("x")))
	in.BaseURL += "missing/" // 404
	if _, err := in.Install(context.Background(), nil); err == nil {
		t.Fatal("want a download error")
	}
	if _, err := os.Stat(in.Dir); !os.IsNotExist(err) {
		t.Fatalf("the empty install dir must be removed: %v", err)
	}
}
