package pgmanaged

import (
	"context"
	"fmt"
	"math/rand"
	"slices"
	"testing"
)

// TestNeverActsOnUnlabeledResources is the isolation property: whatever mix of
// foreign resources exists (with and without our names, running or not, on any
// port) and whatever sequence of operations runs, no mutating command ever
// targets a resource without our label, and foreign resources end unchanged.
func TestNeverActsOnUnlabeledResources(t *testing.T) {
	ctx := context.Background()
	exercised := 0
	for seed := int64(0); seed < 400; seed++ {
		rng := rand.New(rand.NewSource(seed))
		f := newFx(t)
		cfg := f.m.Cfg

		// A random world: our three names may be taken by foreign, owned or no resource.
		pick := func() int { return rng.Intn(3) } // 0 absent, 1 foreign, 2 ours
		switch pick() {
		case 1:
			f.eng.Containers[cfg.Container] = &FakeContainer{ID: "f0" + fmt.Sprint(seed), Name: cfg.Container, Image: "postgres:15",
				Labels: map[string]string{"com.docker.compose.project": "mine"}, Running: rng.Intn(2) == 0, HostIP: "0.0.0.0", HostPort: "5432"}
		case 2:
			f.eng.Containers[cfg.Container] = &FakeContainer{ID: "0w" + fmt.Sprint(seed), Name: cfg.Container, Image: cfg.Image,
				Labels: map[string]string{LabelManaged: "true", LabelRole: RoleContainer}, Running: rng.Intn(2) == 0, HostIP: "127.0.0.1", HostPort: "54329", Volumes: []string{cfg.Volume}}
		}
		switch pick() {
		case 1:
			f.eng.Volumes[cfg.Volume] = map[string]string{}
		case 2:
			f.eng.Volumes[cfg.Volume] = map[string]string{LabelManaged: "true", LabelRole: RoleVolume}
		}
		switch pick() {
		case 1:
			f.eng.Networks[cfg.Network] = map[string]string{"x": "y"}
		case 2:
			f.eng.Networks[cfg.Network] = map[string]string{LabelManaged: "true", LabelRole: RoleNetwork}
		}
		// Unrelated resources of the user: they must never even be inspected for mutation.
		f.eng.Containers["their-postgres"] = &FakeContainer{ID: "t1", Name: "their-postgres", Labels: map[string]string{}, Running: true, HostPort: "5432", HostIP: "0.0.0.0"}
		f.eng.Volumes["their-pgdata"] = map[string]string{}
		for p := DefaultPort; p < DefaultPort+rng.Intn(5); p++ {
			f.busy[p] = true
		}
		if rng.Intn(4) == 0 {
			_ = WriteCredentials(f.home, Credentials{Password: "pw", URL: cfg.DatabaseURL("pw", DefaultPort)})
		}

		before := f.eng.Snapshot()
		var trace []string
		for i := 0; i < 8; i++ {
			switch op := rng.Intn(9); op {
			case 0:
				_, _ = f.m.Install(ctx)
				trace = append(trace, "install")
			case 1:
				_ = f.m.Start(ctx)
				trace = append(trace, "start")
			case 2:
				_ = f.m.Stop(ctx)
				trace = append(trace, "stop")
			case 3:
				_, _ = f.m.Remove(ctx, RemoveOptions{})
				trace = append(trace, "remove")
			case 4:
				_, _ = f.m.Remove(ctx, RemoveOptions{Purge: true, Confirmed: rng.Intn(2) == 0})
				trace = append(trace, "purge")
			case 5:
				_, _ = f.m.Backup(ctx)
				trace = append(trace, "backup")
			case 6:
				_, _ = f.m.Logs(ctx, 5)
				trace = append(trace, "logs")
			case 7:
				_ = f.m.Verify(ctx)
				_, _ = f.m.Status(ctx)
				_, _ = f.m.Plan(ctx)
				trace = append(trace, "read")
			case 8:
				f.eng.NotReady = rng.Intn(5) == 0
				f.m.Cfg.Port = []int{0, 0, 54333, 54334}[rng.Intn(4)]
				trace = append(trace, "tweak")
			}
		}
		exercised += len(f.eng.Mutations)
		if bad := f.eng.Unlabeled(); len(bad) != 0 {
			t.Fatalf("seed %d trace %v: mutation on an unlabeled resource: %+v", seed, trace, bad)
		}
		// Foreign resources are exactly as they were, apart from ones we replaced
		// (never possible) — compare the snapshots.
		if after := f.eng.Snapshot(); !slices.Equal(before, after) {
			t.Fatalf("seed %d trace %v: foreign resources changed:\nbefore %v\nafter  %v", seed, trace, before, after)
		}
	}
	if exercised < 500 {
		t.Fatalf("the property must actually exercise mutations on our own resources, saw %d", exercised)
	}
}
