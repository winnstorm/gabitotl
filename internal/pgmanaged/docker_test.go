package pgmanaged_test

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/winnstorm/gabitotl/internal/pgmanaged"
	"github.com/winnstorm/gabitotl/internal/pgmanaged/pgtest"
)

// These tests drive the real container engine. They are opt-in
// (GABITOTL_TEST_DOCKER=1) and create only throwaway resources with a random
// unique name that carry our label (or, for the "foreign" fixture, a test label
// and never the managed one), all removed at the end.
func dockerManager(t *testing.T) *pgmanaged.Manager {
	t.Helper()
	if !pgtest.DockerEnabled() {
		t.Skip("set GABITOTL_TEST_DOCKER=1 to run the container engine tests")
	}
	cfg := pgtest.ThrowawayConfig(t.TempDir())
	cfg.Port = pgtest.RandomPort()
	m := pgmanaged.New(cfg)
	m.ReadyTimeout = 3 * time.Minute
	cleanup := func() {
		_, _ = m.Remove(context.Background(), pgmanaged.RemoveOptions{Purge: true, Confirmed: true})
	}
	t.Cleanup(cleanup)
	return m
}

func TestDockerLifecycle(t *testing.T) {
	m := dockerManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	defer func() { _, _ = m.Remove(context.Background(), pgmanaged.RemoveOptions{Purge: true, Confirmed: true}) }()

	plan, err := m.Plan(ctx)
	if err != nil || plan.Err() != nil || !strings.Contains(plan.Render(), "create container "+m.Cfg.Container) {
		t.Fatalf("plan: %v %v\n%s", err, plan.Err(), plan.Render())
	}
	res, err := m.Install(ctx)
	if err != nil || !res.Created || !res.Ready {
		t.Fatalf("install: %+v %v", res, err)
	}
	if rep := m.Verify(ctx); !rep.OK() {
		t.Fatalf("verify: %+v", rep)
	}
	again, err := m.Install(ctx)
	if err != nil || again.Created || !again.Ready {
		t.Fatalf("a second install must change nothing: %+v %v", again, err)
	}

	creds, err := pgmanaged.ReadCredentials(m.Cfg.Home)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, creds.URL)
	if err != nil {
		t.Fatalf("connect with the generated credentials: %v", err)
	}
	var one int
	if err := conn.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "CREATE TABLE probe (n int); INSERT INTO probe VALUES (42)"); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)

	bk, err := m.Backup(ctx)
	if err != nil || bk.Bytes == 0 {
		t.Fatalf("backup: %+v %v", bk, err)
	}
	head, _ := os.ReadFile(bk.Path)
	if !strings.HasPrefix(string(head), "PGDMP") {
		t.Fatal("the dump must be a custom-format pg_dump archive")
	}

	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if st, _ := m.Status(ctx); !st.Installed || st.Running {
		t.Fatalf("after stop: %+v", st)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	// The data survives a stop/start.
	conn, err = pgx.Connect(ctx, creds.URL)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := conn.QueryRow(ctx, "SELECT n FROM probe").Scan(&n); err != nil || n != 42 {
		t.Fatalf("data after restart: %d %v", n, err)
	}
	conn.Close(ctx)
	if logs, err := m.Logs(ctx, 20); err != nil || logs == "" {
		t.Fatalf("logs: %q %v", logs, err)
	}

	// Default removal keeps the data volume; purging deletes it.
	r, err := m.Remove(ctx, pgmanaged.RemoveOptions{})
	if err != nil || len(r.Kept) != 1 {
		t.Fatalf("remove: %+v %v", r, err)
	}
	if st, _ := m.Status(ctx); st.Installed || !st.VolumeOK {
		t.Fatalf("container gone, volume kept: %+v", st)
	}
	back, err := m.Install(ctx)
	if err != nil || !back.Created {
		t.Fatalf("reinstall over the kept volume: %+v %v", back, err)
	}
	conn, err = pgx.Connect(ctx, creds.URL)
	if err != nil {
		t.Fatalf("the kept volume must reopen with the same credentials: %v", err)
	}
	if err := conn.QueryRow(ctx, "SELECT n FROM probe").Scan(&n); err != nil || n != 42 {
		t.Fatalf("data after remove + install: %d %v", n, err)
	}
	conn.Close(ctx)
	if _, err := m.Remove(ctx, pgmanaged.RemoveOptions{Purge: true, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	if st, _ := m.Status(ctx); st.Installed || st.VolumeOK || st.NetworkOK {
		t.Fatalf("after purge nothing of ours remains: %+v", st)
	}
}

func TestDockerRefusesAForeignContainerWithOurName(t *testing.T) {
	m := dockerManager(t)
	ctx := context.Background()
	e, err := m.Detect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// A "foreign" container: it carries a test label, never io.gabitotl.managed.
	run := func(args ...string) error {
		_, err := m.Run.Run(ctx, pgmanaged.Call{Bin: e.Path, Args: args})
		return err
	}
	if err := run("create", "--name", m.Cfg.Container, "--label", "io.gabitotl.test=foreign-fixture", "alpine:latest", "sleep", "60"); err != nil {
		t.Skipf("cannot create the fixture container (alpine image missing?): %v", err)
	}
	defer func() { _ = run("rm", "-f", m.Cfg.Container) }()

	if _, err := m.Install(ctx); !errors.Is(err, pgmanaged.ErrNotManaged) {
		t.Fatalf("install over a foreign container must refuse: %v", err)
	}
	for name, op := range map[string]func() error{
		"start": func() error { return m.Start(ctx) },
		"stop":  func() error { return m.Stop(ctx) },
		"remove": func() error {
			_, err := m.Remove(ctx, pgmanaged.RemoveOptions{Purge: true, Confirmed: true})
			return err
		},
	} {
		if err := op(); !errors.Is(err, pgmanaged.ErrNotManaged) {
			t.Errorf("%s: %v", name, err)
		}
	}
	res, err := m.Run.Run(ctx, pgmanaged.Call{Bin: e.Path, Args: []string{"inspect", "--format", "{{.State.Status}} {{.Name}}", m.Cfg.Container}})
	if err != nil || !strings.HasPrefix(string(res.Stdout), "created ") {
		t.Fatalf("the foreign container must be untouched, got %q %v", res.Stdout, err)
	}
	st, _ := m.Status(ctx)
	if len(st.Conflicts) != 1 {
		t.Fatalf("status must show the conflict: %+v", st)
	}
}

func TestDockerPortInUse(t *testing.T) {
	m := dockerManager(t)
	ln, err := net.Listen("tcp", net.JoinHostPort(pgmanaged.Bind, "0"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	m.Cfg.Port = port
	if port < 1024 {
		t.Skip("unexpected ephemeral port " + strconv.Itoa(port))
	}
	if _, err := m.Install(context.Background()); !errors.Is(err, pgmanaged.ErrPortInUse) {
		t.Fatalf("a busy port must be refused before anything is created: %v", err)
	}
	if st, _ := m.Status(context.Background()); st.Installed || st.VolumeOK || st.NetworkOK {
		t.Fatalf("nothing may have been created: %+v", st)
	}
}
