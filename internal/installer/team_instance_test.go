package installer

import (
	"context"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/pgmanaged"
)

func TestHostRecordsTheNamedInstanceInMemoryMD(t *testing.T) {
	r := newTeamRig(t)
	o := hostOpts()
	o.ServerInstance = "trial"
	inv, pl, err := r.plan(o)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	l := gabitotl.NewLayout(r.root)
	cfg, err := gabitotl.LoadMemory(l)
	if err != nil || cfg.Instance != "trial" || cfg.Role != "host" {
		t.Fatalf("%+v %v", cfg, err)
	}
	if !strings.Contains(slurp(t, l.Memory()), "instance: trial") {
		t.Fatalf("memory.md:\n%s", slurp(t, l.Memory()))
	}
	if res.Team.Instance != "trial" {
		t.Fatalf("the result must carry the instance for the hints: %+v", res.Team)
	}
	if _, ok := r.eng.Containers[pgmanaged.DefaultContainer+"-trial"]; !ok {
		t.Fatal("the named instance must be the one installed")
	}
	if err := gabitotl.ValidateNoSecrets([]byte(slurp(t, l.Memory()))); err != nil {
		t.Fatal(err)
	}

	// Re-running with the same instance changes nothing; dropping it is a change
	// (memory.md must stop pointing at an instance this setup no longer serves).
	if _, p2, _ := r.plan(o); p2.Mutates() {
		t.Fatalf("an identical re-run must not mutate: %+v", p2.Items)
	}
	o2 := hostOpts()
	if _, p3, _ := r.plan(o2); !p3.Mutates() {
		t.Fatal("changing the instance is a change")
	}
}

func TestDefaultInstanceLeavesMemoryMDWithoutAnInstance(t *testing.T) {
	r := newTeamRig(t)
	inv, pl, _ := r.plan(hostOpts())
	if _, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(slurp(t, gabitotl.NewLayout(r.root).Memory()), "instance:") {
		t.Fatal("the default instance must not be written")
	}
}

func TestDoctorChecksTheInstanceRecordedInMemoryMD(t *testing.T) {
	r := newTeamRig(t)
	o := hostOpts()
	o.ServerInstance = "trial"
	inv, pl, _ := r.plan(o)
	if _, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{}); err != nil {
		t.Fatal(err)
	}
	var asked []string
	inner := r.env.Team.Manager
	r.env.Team.Manager = func(instance, engine string, port int) (*pgmanaged.Manager, error) {
		asked = append(asked, instance)
		return inner(instance, engine, port)
	}
	d := Doctor(context.Background(), r.env)
	if len(asked) == 0 || asked[len(asked)-1] != "trial" {
		t.Fatalf("doctor must inspect the recorded instance, asked: %v", asked)
	}
	var detail string
	for _, s := range d.Sections {
		for _, l := range s.Lines {
			if l.Code == DocMemTeamContainer {
				detail = l.Detail
			}
		}
	}
	if !strings.Contains(detail, "installed yes") || !strings.Contains(detail, "running yes") {
		t.Fatalf("the named container must be the one reported: %q", detail)
	}
}

func TestMemoryMDWithoutInstanceStillParses(t *testing.T) {
	// A memory.md written before the instance field existed.
	r := newTeamRig(t)
	l := gabitotl.NewLayout(r.root)
	writeFile(t, l.Memory(), "---\nmode: team\nprovider: memory-mcp\nproject_key: shop\nurl: http://127.0.0.1:8765/mcp\ntoken: env:GABITOTL_MEMORY_TOKEN\nrole: host\n---\n\n# Memory\n")
	cfg, err := gabitotl.LoadMemory(l)
	if err != nil || cfg.Instance != "" {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestInstanceMustBeAPlainName(t *testing.T) {
	for _, bad := range []string{"env:TRIAL", "Trial", "a b", "-x", strings.Repeat("a", 32), "a/b"} {
		c := gabitotl.MemoryConfig{Mode: "team", Provider: "memory-mcp", ProjectKey: "shop", Instance: bad}
		if c.Validate() == nil {
			t.Errorf("instance %q must be rejected", bad)
		}
	}
	c := gabitotl.MemoryConfig{Mode: "team", Provider: "memory-mcp", ProjectKey: "shop", Instance: "trial-2"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
