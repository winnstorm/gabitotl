package importer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) (gabitotl.Layout, Env) {
	t.Helper()
	root, home := t.TempDir(), t.TempDir()
	if _, err := gabitotl.Init(root, gabitotl.InitOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, ".kiro/steering/tech.md"), "---\ninclusion: always\n---\n# Tech\nGo 1.26, stdlib first.\n")
	write(t, filepath.Join(root, ".kiro/steering/secrets.md"), "# Keys\napi_key: sk-abcdefghijklmnopqrstuvwxyz123456\n")
	write(t, filepath.Join(root, ".kiro/steering/gabitotl.md"), "# ours\nignored ignored ignored ignored\n")
	write(t, filepath.Join(root, ".kiro/specs/login/requirements.md"), "# Requirements\nUsers must log in with SSO.\n")
	write(t, filepath.Join(root, ".kiro/settings/mcp.json"), `{"mcpServers":{"github":{"command":"x","env":{"TOKEN":"hunter2hunter2"}}}}`)
	write(t, filepath.Join(root, "CLAUDE.md"), "# Notes\nPreamble that is long enough to keep around.\n\n## Commands\nRun go test ./... before every commit please.\n\n<!-- gabitotl:start persona -->\nGABITO PERSONA TEXT\n<!-- gabitotl:end persona -->\n<!-- gentle-ai:persona -->\nGENTLEMAN TEXT\n<!-- /gentle-ai:persona -->\n")
	write(t, filepath.Join(home, ".gentle-ai/state.json"), `{"version":"4.1","installed_agents":["claude-code",{"name":"opencode"}],"components":["persona","skills"],"secret":"x"}`)
	write(t, filepath.Join(root, ".engram/manifest.json"), `{"project":"p"}`)
	write(t, filepath.Join(root, "odd/tasks/feat.md"), "# feat\n- [x] done\n- [ ] write tests\n")
	env := Env{Home: home, ProjectRoot: root, ProjectKey: "p",
		Getenv:   func(string) string { return "" },
		LookPath: func(string) (string, error) { return "", os.ErrNotExist },
		Projects: func(context.Context) ([]ProjectInfo, error) { return []ProjectInfo{{"p", 42}}, nil },
		Now:      func() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) }}
	return gabitotl.NewLayout(root), env
}

func find(o []Outcome, name string) Outcome {
	for _, x := range o {
		if x.Source == name {
			return x
		}
	}
	return Outcome{}
}

func TestCollectExtractsUsefulKnowledgeAndSkipsSecrets(t *testing.T) {
	_, env := fixture(t)
	out, err := Run(context.Background(), gabitotl.NewLayout(env.ProjectRoot), env, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	k := find(out, "kiro")
	var titles []string
	for _, it := range k.Items {
		titles = append(titles, it.Title)
		if strings.Contains(it.Body, "hunter2") || strings.Contains(it.Body, "inclusion") {
			t.Fatalf("leaked config or secret: %+v", it)
		}
	}
	joined := strings.Join(titles, "|")
	if !strings.Contains(joined, "steering: tech") || !strings.Contains(joined, "spec login: requirements") || !strings.Contains(joined, "Kiro MCP servers") {
		t.Fatalf("missing items: %v", titles)
	}
	if strings.Contains(joined, "ours") || strings.Contains(joined, "secrets") || len(k.Skipped) != 1 {
		t.Fatalf("secret/own file not skipped: %v %v", titles, k.Skipped)
	}
	ins := find(out, "instructions")
	for _, it := range ins.Items {
		if strings.Contains(it.Body, "GABITO") || strings.Contains(it.Body, "GENTLEMAN") {
			t.Fatalf("managed blocks must not be imported: %+v", it)
		}
	}
	if len(ins.Items) < 2 || find(out, "gentle-ai").Status != StatusWouldApply || find(out, "engram").Items[0].Body == "" {
		t.Fatalf("%+v", out)
	}
	if !strings.Contains(find(out, "engram").Items[0].Body, "42 observations") {
		t.Fatalf("observation count missing: %s", find(out, "engram").Items[0].Body)
	}
	g := find(out, "gentle-ai")
	if !strings.Contains(g.Items[0].Body, "claude-code, opencode") || strings.Contains(g.Items[0].Body, `"secret"`) {
		t.Fatalf("gentle-ai: %s", g.Items[0].Body)
	}
}

func TestDetectIsIndependent(t *testing.T) {
	_, env := fixture(t)
	if err := os.RemoveAll(filepath.Join(env.Home, ".gentle-ai")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(env.ProjectRoot, "odd")); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, s := range Sources() {
		found[s.Name()] = s.Detect(env).Found
	}
	if found["gentle-ai"] || !found["engram"] || !found["kiro"] {
		t.Fatalf("engram must be detected without gentle-ai: %v", found)
	}
}

func TestRegistrySkipsUnchangedAndReimportsOnChange(t *testing.T) {
	l, env := fixture(t)
	var saved []Knowledge
	sink := func(_ context.Context, k Knowledge) error { saved = append(saved, k); return nil }
	ctx := context.Background()
	out, err := Run(ctx, l, env, Options{Only: []string{"kiro"}, Sink: sink})
	if err != nil || find(out, "kiro").Status != StatusImported || len(saved) == 0 {
		t.Fatal(err, out)
	}
	reg, _ := LoadRegistry(l)
	e := reg.Imports[0]
	if e.Origin != "kiro" || e.Hash == "" || !e.Memory || e.Date != "2026-10-05" || len(e.Imported) == 0 {
		t.Fatalf("%+v", e)
	}
	n := len(saved)
	env.Now = func() time.Time { return time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC) }
	out, _ = Run(ctx, l, env, Options{Only: []string{"kiro"}, Sink: sink})
	if find(out, "kiro").Status != StatusUnchanged || len(saved) != n {
		t.Fatalf("unchanged source re-imported: %+v", out)
	}
	reg, _ = LoadRegistry(l)
	if reg.Imports[0].LastReview != "2026-10-09" || reg.Imports[0].Date != "2026-10-05" {
		t.Fatalf("last_review not advanced: %+v", reg.Imports[0])
	}
	write(t, filepath.Join(env.ProjectRoot, ".kiro/steering/tech.md"), "# Tech\nNow Rust.\n")
	out, _ = Run(ctx, l, env, Options{Only: []string{"kiro"}, Sink: sink})
	if find(out, "kiro").Status != StatusImported || len(saved) <= n {
		t.Fatalf("changed source not re-imported: %+v", out)
	}
}

func TestDryRunWritesNothingAndSinkFailureIsNotRecorded(t *testing.T) {
	l, env := fixture(t)
	before, _ := os.ReadFile(l.Imports())
	if _, err := Run(context.Background(), l, env, Options{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(l.Imports()); string(after) != string(before) {
		t.Fatal("dry run wrote imports.md")
	}
	fail := func(context.Context, Knowledge) error { return errors.New("down") }
	out, _ := Run(context.Background(), l, env, Options{Only: []string{"kiro"}, Sink: fail})
	if find(out, "kiro").Status != StatusFailed {
		t.Fatalf("%+v", out)
	}
	if reg, _ := LoadRegistry(l); len(reg.Imports) != 0 {
		t.Fatal("failed import must not be recorded")
	}
}

func TestImportersNeverModifySourceFiles(t *testing.T) {
	l, env := fixture(t)
	snap := map[string]string{}
	collect := func() map[string]string {
		m := map[string]string{}
		for _, base := range []string{env.ProjectRoot, env.Home} {
			_ = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() && !strings.Contains(p, ".gabitotl") {
					b, _ := os.ReadFile(p)
					m[p] = string(b)
				}
				return nil
			})
		}
		return m
	}
	snap = collect()
	if _, err := Run(context.Background(), l, env, Options{Sink: func(context.Context, Knowledge) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	after := collect()
	if len(after) != len(snap) {
		t.Fatal("files added or removed outside .gabitotl")
	}
	for p, c := range snap {
		if after[p] != c {
			t.Fatalf("%s modified", p)
		}
	}
}
