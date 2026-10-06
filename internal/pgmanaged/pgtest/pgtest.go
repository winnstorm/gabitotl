// Package pgtest gives tests an isolated PostgreSQL, and only when asked to.
//
// Two opt-in sources, checked in this order:
//
//   - GABITOTL_TEST_PG_DSN points at a throwaway database (never one that
//     matters): every test gets its own schema (gtl_test_<random>) which is
//     dropped afterwards, and nothing else in that database is touched.
//   - GABITOTL_TEST_DOCKER=1 starts ONE throwaway container for the test
//     binary, named gabitotl-test-<random>-pg with its own volume and network,
//     all carrying the GabitoTL label, and removes all of it when the binary
//     exits. It never uses the default instance names. The image is the
//     managed default unless GABITOTL_TEST_PG_IMAGE overrides it; nothing is
//     pulled unless the engine decides it must.
//
// Without either variable Skip tells the test to skip.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/winnstorm/gabitotl/internal/pgmanaged"
)

// Environment switches.
const (
	EnvDSN    = "GABITOTL_TEST_PG_DSN"
	EnvDocker = "GABITOTL_TEST_DOCKER"
	EnvImage  = "GABITOTL_TEST_PG_IMAGE"
)

var (
	mu       sync.Mutex
	inst     *pgmanaged.Manager
	instDSN  string
	instErr  error
	started  bool
	instHome string
)

// DockerEnabled reports whether docker-backed tests were requested.
func DockerEnabled() bool { return os.Getenv(EnvDocker) == "1" }

// Enabled reports whether any PostgreSQL source is configured.
func Enabled() bool { return os.Getenv(EnvDSN) != "" || DockerEnabled() }

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ThrowawayConfig returns a managed-instance configuration whose every name is
// unique to this call. Nothing in it can collide with the default instance.
func ThrowawayConfig(home string) pgmanaged.Config {
	id := randHex(4)
	cfg := pgmanaged.DefaultConfig(home)
	cfg.Container = "gabitotl-test-" + id + "-pg"
	cfg.Volume = "gabitotl-test-" + id + "-data"
	cfg.Network = "gabitotl-test-" + id + "-net"
	if img := os.Getenv(EnvImage); img != "" {
		cfg.Image = img
	}
	return cfg
}

// RandomPort picks a port in 54400-54999, away from the default 54329 range.
func RandomPort() int {
	n, _ := rand.Int(rand.Reader, big.NewInt(600))
	return 54400 + int(n.Int64())
}

func startInstance() (string, error) {
	home, err := os.MkdirTemp("", "gabitotl-pgtest-*")
	if err != nil {
		return "", err
	}
	instHome = home
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		cfg := ThrowawayConfig(home)
		cfg.Port = RandomPort()
		m := pgmanaged.New(cfg)
		m.ReadyTimeout = 3 * time.Minute
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		_, err := m.Install(ctx)
		cancel()
		if err != nil {
			lastErr = err
			// Remove whatever this attempt created (label-checked) before retrying.
			_, _ = m.Remove(context.Background(), pgmanaged.RemoveOptions{Purge: true, Confirmed: true})
			continue
		}
		creds, err := pgmanaged.ReadCredentials(cfg.SecretsFile())
		if err != nil {
			_, _ = m.Remove(context.Background(), pgmanaged.RemoveOptions{Purge: true, Confirmed: true})
			return "", err
		}
		inst = m
		return creds.URL, nil
	}
	return "", fmt.Errorf("could not start a throwaway PostgreSQL: %w", lastErr)
}

// Close removes the throwaway instance (if one was started). Call it from
// TestMain through Main, or defer it.
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if inst != nil {
		_, _ = inst.Remove(context.Background(), pgmanaged.RemoveOptions{Purge: true, Confirmed: true})
		inst = nil
	}
	if instHome != "" {
		_ = os.RemoveAll(instHome)
		instHome = ""
	}
	started, instDSN, instErr = false, "", nil
}

// Main is a TestMain body: it runs the tests and always tears the instance down.
func Main(m *testing.M) int {
	defer Close()
	return m.Run()
}

func baseDSN(t testing.TB) string {
	if dsn := os.Getenv(EnvDSN); dsn != "" {
		return dsn
	}
	if !DockerEnabled() {
		t.Skipf("set %s=1 (throwaway container) or %s (isolated database) to run PostgreSQL tests", EnvDocker, EnvDSN)
	}
	mu.Lock()
	defer mu.Unlock()
	if !started {
		instDSN, instErr = startInstance()
		started = true
	}
	if instErr != nil {
		t.Fatalf("throwaway PostgreSQL: %v", instErr)
	}
	return instDSN
}

// Database returns a DSN whose search_path is a brand-new schema, dropped when
// the test ends. Each test therefore sees an empty database of its own.
func Database(t testing.TB) string {
	t.Helper()
	dsn := baseDSN(t)
	schema := "gtl_test_" + randHex(6)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to the test database: %v", err)
	}
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		conn.Close(ctx)
		t.Fatalf("create test schema: %v", err)
	}
	conn.Close(ctx)
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), dsn)
		if err != nil {
			t.Errorf("cleanup: %v", err)
			return
		}
		defer c.Close(context.Background())
		if _, err := c.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
			t.Errorf("cleanup schema: %v", err)
		}
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("test DSN is not a URL: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}
