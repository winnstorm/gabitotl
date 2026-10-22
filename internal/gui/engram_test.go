package gui

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/engraminstall"
)

type engDL struct {
	body  []byte
	calls int
}

func (d *engDL) Download(_ context.Context, _ string, dst io.Writer, _ int64) error {
	d.calls++
	_, err := dst.Write(d.body)
	return err
}

// withEngram installs a Linux Engram installer over a fake release; the
// installed binary becomes visible to the rig's PATH, like the real one does
// through the process environment.
func (r *rig) withEngram(tamper bool) (*engraminstall.Installer, *engDL) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "engram", Mode: 0o755, Size: 4, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("bin!"))
	_ = tw.Close()
	_ = gz.Close()
	h := sha256.Sum256(buf.Bytes())
	body := buf.Bytes()
	if tamper {
		body = append(append([]byte{}, body...), 0)
	}
	dl := &engDL{body: body}
	in := &engraminstall.Installer{
		Ports: engraminstall.Ports{
			Download: dl,
			Look: func(n string) (string, error) {
				if r.bins[n] {
					return "/fake/bin/" + n, nil
				}
				return "", os.ErrNotExist
			},
			Run:            func(context.Context, string, ...string) (string, error) { return "engram 1.0.0", nil },
			Getenv:         func(string) string { return "" },
			SetProcessPath: func(string) { r.bins["engram"] = true },
		},
		GOOS: "linux", GOARCH: "amd64", Dir: filepath.Join(r.t.TempDir(), "bin"),
		Table: map[string]engraminstall.Asset{"linux/amd64": {Name: "e.tar.gz", SHA256: hex.EncodeToString(h[:])}},
		Token: func() string { return "t" },
	}
	r.runner.Handlers["engram doctor"] = func([]string) (string, error) {
		return `{"status":"ok","project":"shop","checks":[{"check_id":"db","result":"ok","message":"fine"}]}`, nil
	}
	r.runner.Handlers["engram projects"] = func([]string) (string, error) { return "Projects:\n", nil }
	r.runner.Handlers["engram export"] = func(a []string) (string, error) {
		write(r.t, a[1], `{"sessions":[],"observations":[]}`)
		return "Exported to " + a[1] + "\n  Sessions: 0\n  Observations: 0\n  Prompts: 0\n", nil
	}
	r.svc.d.Engram = in
	return in, dl
}

func hasStep(p Plan, id string) bool {
	for _, s := range p.Steps {
		if s.ID == id {
			return true
		}
	}
	return false
}

func TestPlanHasEngramStepOnlyWhenNeeded(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.withEngram(false)
	pl, err := r.svc.PlanInstall(ctx, r.answers())
	if err != nil {
		t.Fatal(err)
	}
	if !hasStep(pl, "engram") {
		t.Fatalf("solo memory without Engram must plan its install: %+v", pl.Steps)
	}
	team := r.answers()
	team.Memory = MemoryAnswers{Mode: "team", Role: "client", URL: "https://mem.example.com", TokenEnv: "MEMORY_TOKEN"}
	if pl, err = r.svc.PlanInstall(ctx, team); err == nil && hasStep(pl, "engram") {
		t.Fatal("team memory does not use Engram")
	}
	r.bins["engram"] = true
	if pl, _ = r.svc.PlanInstall(ctx, r.answers()); hasStep(pl, "engram") {
		t.Fatal("an Engram already on PATH is never planned")
	}
}

func TestWizardInstallsEngramThenRegistersItsServer(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	in, dl := r.withEngram(false)
	res := r.install(r.answers())
	if dl.calls != 1 {
		t.Fatalf("downloads = %d", dl.calls)
	}
	if b, err := os.ReadFile(in.Target()); err != nil || string(b) != "bin!" {
		t.Fatalf("binary = %q, %v", b, err)
	}
	var eng *StepResult
	for i := range res.Steps {
		if res.Steps[i].ID == "engram" {
			eng = &res.Steps[i]
		}
	}
	if eng == nil || eng.Status != StatusOK {
		t.Fatalf("steps = %+v", res.Steps)
	}
	cfg, _ := os.ReadFile(filepath.Join(r.home, ".claude.json"))
	if !strings.Contains(string(cfg), `"engram"`) {
		t.Fatalf("the agent did not get Engram's own server after the install:\n%s", cfg)
	}
	st, err := r.svc.Engram(ctx)
	if err != nil || !st.Installed || st.NeedsWork {
		t.Fatalf("status %+v err=%v", st, err)
	}
}

func TestWizardKeepsGoingWhenEngramFailsClosed(t *testing.T) {
	r := newRig(t)
	in, _ := r.withEngram(true)
	res := r.install(r.answers())
	var st string
	for _, s := range res.Steps {
		if s.ID == "engram" {
			st = s.Status
		}
	}
	if st != StatusError {
		t.Fatalf("a hash mismatch must be reported: %+v", res.Steps)
	}
	if _, err := os.Stat(in.Target()); !os.IsNotExist(err) {
		t.Fatal("binary installed despite the hash mismatch")
	}
}

func TestDoctorOffersEngramInstallAndFixRuns(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	in, _ := r.withEngram(false)
	r.project()
	rep, err := r.svc.Doctor(ctx, r.root)
	if err != nil {
		t.Fatal(err)
	}
	var fix *FixAction
	for _, sec := range rep.Sections {
		for _, l := range sec.Lines {
			if l.Fix != nil && l.Fix.ID == FixEngramInstall {
				fix = l.Fix
			}
		}
	}
	if fix == nil || !fix.Mutates {
		t.Fatalf("no Engram install fix in the doctor: %+v", rep)
	}
	req := FixRequest{Root: r.root, ID: FixEngramInstall}
	if _, err := r.svc.RunFix(ctx, req, "", nil); !errors.Is(err, ErrPlanRequired) {
		t.Fatalf("a mutating fix needs its plan id, got %v", err)
	}
	pl, err := r.svc.PlanFix(ctx, req)
	if err != nil || !pl.Mutates {
		t.Fatalf("plan %+v err=%v", pl, err)
	}
	if res, err := r.svc.RunFix(ctx, req, pl.ID, nil); err != nil || !res.OK {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if _, err := os.Stat(in.Target()); err != nil {
		t.Fatal(err)
	}
}

func TestEngramStatusUnsupportedSystem(t *testing.T) {
	r := newRig(t)
	st, err := r.svc.Engram(context.Background())
	if err != nil || st.Supported || st.NeedsWork || st.Source != engraminstall.SourceURL {
		t.Fatalf("st=%+v err=%v", st, err)
	}
	if _, err := r.svc.ApplyEngram(context.Background(), "x", nil); err == nil {
		t.Fatal("unsupported systems cannot apply")
	}
}
