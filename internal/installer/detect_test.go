package installer

import (
	"context"
	"strings"
	"testing"

	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

func TestDetectPlatform(t *testing.T) {
	f := newFixture(t)
	p := DetectPlatform(f.env)
	if p.OS != "linux" || p.Arch != "amd64" || p.Shell != "bash" || !p.Git || p.PackageManager != "apt" || p.Home != f.home {
		t.Fatalf("%+v", p)
	}
	f.bins["git"], f.bins["apt"] = false, false
	if p := DetectPlatform(f.env); p.Git || p.PackageManager != "" {
		t.Fatalf("missing tools must not be invented: %+v", p)
	}
}

func TestDetectProject(t *testing.T) {
	f := newFixture(t)
	p := DetectProject(context.Background(), f.env)
	if p.Root != f.root || p.Exists || p.Kind != "existing" || !p.GitRepo || p.Provider != "github" || p.Key == "" {
		t.Fatalf("%+v", p)
	}
	f.env.ProjectRoot = ""
	if p := DetectProject(context.Background(), f.env); p.Root != "" || p.Exists {
		t.Fatalf("no root means no project: %+v", p)
	}
}

func TestDetectRuntimesSeesHistoryAndConflicts(t *testing.T) {
	f := newFixture(t)
	rs := DetectRuntimes(f.env)
	got := map[string]RuntimeStatus{}
	for _, r := range rs {
		got[r.Name] = r
	}
	if len(rs) != 4 || !got["claude-code"].Usable() || !got["kiro-cli"].Usable() {
		t.Fatalf("%+v", rs)
	}
	if len(got["claude-code"].Conflicts) == 0 {
		t.Fatalf("Gentle-AI persona block must be reported as a conflict: %+v", got["claude-code"])
	}
	f.bins["claude"] = false
	for _, r := range DetectRuntimes(f.env) {
		if r.Name == "claude-code" && r.State != rt.StateDetectedHistory {
			t.Fatalf("config without binary is history, got %s", r.State)
		}
	}
}

func TestDetectMemoryAdoptsEngramAndWarnsOnTopology(t *testing.T) {
	f := newFixture(t)
	pr := Project{Root: f.root, Key: "shop"}
	m := DetectMemory(context.Background(), f.env, pr)
	if !m.HasBinary || !m.HasDB || !m.DoctorRan || !m.DoctorOK || m.Observations != 7 || m.ProjectMatch != "shop" || len(m.Warnings) != 0 {
		t.Fatalf("trivial topology must not warn: %+v", m)
	}
	f.vars["ENGRAM_DATA_DIR"] = f.home + "/.engram"
	f.vars["ENGRAM_CLOUD_TOKEN"] = "super-secret-value"
	writeFile(t, f.root+"/.engram/config.json", `{"project":"Shop","autosync":true}`)
	m = DetectMemory(context.Background(), f.env, pr)
	codes := map[string]bool{}
	for _, w := range m.Warnings {
		codes[w.Code] = true
		if strings.Contains(w.Detail, "super-secret-value") {
			t.Fatalf("warning leaks an environment value: %+v", w)
		}
	}
	for _, want := range []string{WarnDataDirOverride, WarnCloudSync, WarnProjectConfig, WarnProjectIdentity} {
		if !codes[want] {
			t.Errorf("missing warning %s in %+v", want, m.Warnings)
		}
	}
}

func TestDetectMemoryWithoutEngramBinary(t *testing.T) {
	f := newFixture(t)
	f.bins["engram"] = false
	m := DetectMemory(context.Background(), f.env, Project{Root: f.root, Key: "shop"})
	if m.HasBinary || m.DoctorRan || !m.HasDB || !m.Present() {
		t.Fatalf("%+v", m)
	}
	if f.runner.Count("engram") != 0 {
		t.Fatal("engram must not run when it is not installed")
	}
}

func TestDetectOtherHarnessReadsGentleState(t *testing.T) {
	f := newFixture(t)
	g := DetectOtherHarness(f.env).GentleAI
	if !g.Found || !g.HasState || !g.HasBackups || g.Version != "4.2.0" || len(g.Agents) != 4 {
		t.Fatalf("%+v", g)
	}
	byID := map[string]string{}
	for _, a := range g.Agents {
		byID[a.ID] = a.Runtime
	}
	if byID["claude-code"] != "claude-code" || byID["windsurf"] != "" {
		t.Fatalf("agents without a V1 adapter must stay unmapped: %+v", g.Agents)
	}
	writeFile(t, f.home+"/.gentle-ai/state.json", "{broken")
	if g := DetectOtherHarness(f.env).GentleAI; g.StateError == "" || !g.Found {
		t.Fatalf("broken state is reported, not fatal: %+v", g)
	}
}

func TestInventoryOrderReadOnlyAndNoSecrets(t *testing.T) {
	f := newFixture(t)
	f.remote = "https://user:SECRETTOKEN@github.com/acme/shop.git"
	f.vars["ENGRAM_CLOUD_TOKEN"] = "ENVVALUE123"
	before := treeHash(t, f.home, f.root)
	var phases []Phase
	inv := BuildInventory(context.Background(), f.env, func(e Event) {
		if e.Status == "start" {
			phases = append(phases, e.Phase)
		}
	})
	want := []Phase{PhasePlatform, PhaseProject, PhaseRuntimes, PhaseMemory, PhaseOtherHarness}
	if strings.Join(phaseNames(phases), ",") != strings.Join(phaseNames(want), ",") {
		t.Fatalf("phases = %v", phases)
	}
	b, err := inv.JSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"SECRETTOKEN", "ENVVALUE123"} {
		if strings.Contains(string(b), leak) {
			t.Fatalf("inventory leaks %s:\n%s", leak, b)
		}
	}
	if treeHash(t, f.home, f.root) != before {
		t.Fatal("detection must not change anything")
	}
	if f.runner.Count("engram export") != 0 || f.runner.Count("gentle-ai") != 0 {
		t.Fatalf("detection may only run read-only commands: %v", f.runner.Log())
	}
}

func phaseNames(p []Phase) []string {
	var o []string
	for _, x := range p {
		o = append(o, string(x))
	}
	return o
}
