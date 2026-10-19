package gui

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/codegraph"
)

type cgPath struct{ v string }

func (p *cgPath) Get() (string, error) { return p.v, nil }
func (p *cgPath) Set(v string) error   { p.v = v; return nil }

type cgDL struct {
	body  []byte
	calls int
}

func (d *cgDL) Download(_ context.Context, _ string, dst io.Writer, _ int64) error {
	d.calls++
	_, err := dst.Write(d.body)
	return err
}

// cgRig swaps the pinned hash for the one of a tiny bundle and installs a fake
// Windows CodeGraph installer into the service.
func (r *rig) withCodeGraph(tamper bool) (*codegraph.Installer, *cgDL) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("codegraph-win32-x64/bin/codegraph.cmd")
	w.Write([]byte("@echo off"))
	zw.Close()
	sum := sha256.Sum256(buf.Bytes())
	old := codegraph.Assets["windows/amd64"]
	codegraph.Assets["windows/amd64"] = codegraph.Asset{Name: old.Name, SHA256: hex.EncodeToString(sum[:])}
	r.t.Cleanup(func() { codegraph.Assets["windows/amd64"] = old })
	body := buf.Bytes()
	if tamper {
		body = append(append([]byte{}, body...), 0)
	}
	dl := &cgDL{body: body}
	installed := false
	in := &codegraph.Installer{
		Ports: codegraph.Ports{
			Download: dl, Path: &cgPath{},
			Look: func(string) (string, error) { return "", os.ErrNotExist },
			Run: func(context.Context, string, ...string) (string, error) {
				if !installed {
					return "", errors.New("missing")
				}
				return "codegraph 1.6.2", nil
			},
		},
		GOOS: "windows", GOARCH: "amd64", Root: filepath.Join(r.t.TempDir(), "codegraph"),
	}
	// Once the launcher exists the fake binary answers --version.
	run := in.Run
	in.Run = func(ctx context.Context, bin string, a ...string) (string, error) {
		if _, err := os.Stat(in.Launcher()); err == nil {
			installed = true
		}
		return run(ctx, bin, a...)
	}
	r.svc.d.CodeGraph = in
	return in, dl
}

func TestWizardInstallsCodeGraphAndRegistersMCP(t *testing.T) {
	r := newRig(t)
	in, _ := r.withCodeGraph(false)
	a := r.answers()
	pl, err := r.svc.PlanInstall(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range pl.Steps {
		found = found || s.ID == "codegraph"
	}
	if !found {
		t.Fatalf("plan has no codegraph step: %+v", pl.Steps)
	}
	res := r.install(a)
	if len(res.Steps) == 0 || res.Steps[0].ID != "codegraph" || res.Steps[0].Status != StatusOK {
		t.Fatalf("steps %+v", res.Steps)
	}
	if _, err := os.Stat(in.Launcher()); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(r.home, ".claude.json"))
	if !strings.Contains(string(cfg), `"codegraph"`) || !strings.Contains(string(cfg), "--mcp") {
		t.Fatalf("claude config lacks the codegraph server:\n%s", cfg)
	}
	st, err := r.svc.CodeGraph(context.Background())
	if err != nil || !st.Installed || st.Version != "1.6.2" || st.Pinned != codegraph.Version || st.NeedsWork {
		t.Fatalf("status %+v err=%v", st, err)
	}
	if len(st.Agents) == 0 || !st.Agents[0].Registered {
		t.Fatalf("agents %+v", st.Agents)
	}
}

func TestWizardStopsWhenCodeGraphFailsClosed(t *testing.T) {
	r := newRig(t)
	in, _ := r.withCodeGraph(true)
	a := r.answers()
	pl, err := r.svc.PlanInstall(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.svc.Install(context.Background(), a, pl.ID, nil)
	if err == nil || len(res.Steps) != 1 || res.Steps[0].Status != StatusError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if _, e := os.Stat(in.Current()); !os.IsNotExist(e) {
		t.Error("CodeGraph installed despite the hash mismatch")
	}
	if _, e := os.Stat(filepath.Join(r.home, ".claude.json")); !os.IsNotExist(e) {
		t.Error("an agent was configured before the required component")
	}
}

func TestCodeGraphRepairIsPlanFirst(t *testing.T) {
	r := newRig(t)
	r.withCodeGraph(false)
	r.install(r.answers())
	cfgPath := filepath.Join(r.home, ".claude.json")
	b, _ := os.ReadFile(cfgPath)
	i := strings.Index(string(b), `"codegraph"`)
	if i < 0 {
		t.Fatal("no entry to break")
	}
	os.WriteFile(cfgPath, []byte(`{"mcpServers":{}}`), 0o600) // the entry vanished
	ctx := context.Background()
	st, _ := r.svc.CodeGraph(ctx)
	if !st.NeedsWork {
		t.Fatalf("status should flag the missing entry: %+v", st)
	}
	if _, err := r.svc.ApplyCodeGraph(ctx, "", nil); !errors.Is(err, ErrPlanRequired) {
		t.Fatalf("err = %v", err)
	}
	pl, err := r.svc.PlanCodeGraph(ctx)
	if err != nil || !pl.Mutates {
		t.Fatalf("plan %+v err=%v", pl, err)
	}
	if _, err := r.svc.ApplyCodeGraph(ctx, pl.ID, nil); err != nil {
		t.Fatal(err)
	}
	if st, _ := r.svc.CodeGraph(ctx); st.NeedsWork {
		t.Fatalf("still needs work: %+v", st)
	}
	again, _ := r.svc.PlanCodeGraph(ctx)
	if again.Mutates {
		t.Errorf("second plan should be a no-op: %+v", again)
	}
}

func TestCodeGraphUnsupportedOS(t *testing.T) {
	r := newRig(t)
	st, err := r.svc.CodeGraph(context.Background())
	if err != nil || st.Supported || st.Hint == "" {
		t.Fatalf("%+v %v", st, err)
	}
	pl, _ := r.svc.PlanCodeGraph(context.Background())
	if pl.Supported || len(pl.Unsupports) == 0 {
		t.Fatalf("%+v", pl)
	}
	if _, err := r.svc.ApplyCodeGraph(context.Background(), pl.ID, nil); !errors.Is(err, ErrUnsupportedOS) {
		t.Fatalf("err = %v", err)
	}
}
