// Package pgmanaged runs the PostgreSQL that backs the GabitoTL Memory MCP
// server, as a dedicated container created and labeled by GabitoTL.
//
// Isolation is the whole point of this package, and it holds by construction:
//
//   - Everything GabitoTL creates (container, volume, network) carries the label
//     io.gabitotl.managed=true. Before any mutating command the target is
//     inspected and acted on only when it carries that label. A resource that
//     has our name but not our label is a conflict: the operation refuses and
//     explains, it never adopts, stops, removes or even modifies it.
//   - Names must start with "gabitotl-", the host port is never 5432, and the
//     published address is loopback only.
//   - The user's PG* variables, DATABASE_URL, POSTGRES_* and ~/.pgpass are never
//     read: the environment handed to the container engine is filtered, and
//     credentials are generated and kept in a 0600 file under the injected home.
//   - The generated password never appears in a process argument: the engine
//     reads it from the environment of the command.
//   - Removing the instance keeps the data volume unless purge is asked for.
//
// The engine is driven through its CLI behind a Runner so every behavior is
// testable with a fake; docker and podman share one code path.
package pgmanaged

import (
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"regexp"
	"strings"
)

// Labels stamped on every resource GabitoTL creates.
const (
	LabelManaged = "io.gabitotl.managed"
	LabelRole    = "io.gabitotl.role"

	RoleContainer = "memory-postgres"
	RoleVolume    = "memory-postgres-data"
	RoleNetwork   = "memory-postgres-net"
)

// Defaults for the managed instance.
const (
	DefaultContainer = "gabitotl-memory-postgres"
	DefaultVolume    = "gabitotl-memory-pgdata"
	DefaultNetwork   = "gabitotl-memory-net"
	// DefaultImage is the pinned official image. PostgreSQL 17 keeps the data
	// directory at /var/lib/postgresql/data, which the volume mount relies on.
	DefaultImage    = "postgres:17-alpine"
	DefaultPort     = 54329
	DefaultDatabase = "gabitotl_memory"
	DefaultUser     = "gabitotl_memory"
	// PortSearchSpan is how many ports after the default are tried when it is busy.
	PortSearchSpan = 70
	containerPort  = 5432
)

// Environment variable names used for the credentials (values never leave the
// 0600 secrets file and the environment of the process that needs them).
const (
	EnvDatabaseURL = "GABITOTL_MEMORY_DATABASE_URL"
	EnvPassword    = "GABITOTL_MEMORY_POSTGRES_PASSWORD"
)

// Errors.
var (
	// ErrNotManaged means a resource has our name but not our label.
	ErrNotManaged = errors.New("pgmanaged: a resource with this name exists but was not created by GabitoTL")
	// ErrNoEngine means neither docker nor podman is usable.
	ErrNoEngine = errors.New("pgmanaged: no container engine available")
	// ErrPortInUse means an explicitly requested port is taken.
	ErrPortInUse = errors.New("pgmanaged: the requested port is already in use")
	// ErrNoCredentials means the data volume exists but its credentials file is gone.
	ErrNoCredentials = errors.New("pgmanaged: credentials file is missing")
	// ErrPurgeUnconfirmed means purge was requested without the confirmation.
	ErrPurgeUnconfirmed = errors.New("pgmanaged: purging the data volume needs explicit confirmation")
)

// Config describes one managed instance. The zero value is not usable: start
// from DefaultConfig.
type Config struct {
	// Engine is "docker", "podman" or "auto".
	Engine    string
	Container string
	Volume    string
	Network   string
	Image     string
	// Port is the host port on loopback. 0 means "the default, or the next free
	// one when it is taken"; a non-zero value must be free or Install fails.
	Port     int
	Database string
	User     string
	// Home is the injected global home (the directory that contains .gabitotl).
	Home string
	// Instance is empty for the default instance. A named instance gets its own
	// container, volume, network and credentials file (see WithInstance).
	Instance string
}

// DefaultConfig returns the default instance rooted at home.
func DefaultConfig(home string) Config {
	return Config{
		Engine: "auto", Container: DefaultContainer, Volume: DefaultVolume, Network: DefaultNetwork,
		Image: DefaultImage, Database: DefaultDatabase, User: DefaultUser, Home: home,
	}
}

// Bind is the only published address: loopback.
const Bind = "127.0.0.1"

var (
	nameRe  = regexp.MustCompile(`^gabitotl-[a-z0-9][a-z0-9_.-]{0,62}$`)
	identRe = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)
	imageRe = regexp.MustCompile(`^[a-z0-9][a-z0-9./_:@-]{0,200}$`)
)

// Validate rejects configurations that could reach a resource GabitoTL does not own.
func (c Config) Validate() error {
	switch c.Engine {
	case "auto", "docker", "podman":
	default:
		return fmt.Errorf("pgmanaged: engine must be docker, podman or auto, got %q", c.Engine)
	}
	for _, n := range []struct{ what, v string }{{"container", c.Container}, {"volume", c.Volume}, {"network", c.Network}} {
		if !nameRe.MatchString(n.v) {
			return fmt.Errorf("pgmanaged: %s name %q must start with \"gabitotl-\" (lowercase letters, digits, . _ -)", n.what, n.v)
		}
	}
	if c.Container == c.Volume || c.Container == c.Network || c.Volume == c.Network {
		return errors.New("pgmanaged: container, volume and network need distinct names")
	}
	if !imageRe.MatchString(c.Image) || strings.HasPrefix(c.Image, "-") {
		return fmt.Errorf("pgmanaged: image %q is not valid", c.Image)
	}
	if c.Port != 0 {
		if c.Port == containerPort {
			return fmt.Errorf("pgmanaged: port %d is the standard PostgreSQL port and is never used by the managed instance", containerPort)
		}
		if c.Port < 1024 || c.Port > 65535 {
			return fmt.Errorf("pgmanaged: port %d is out of range (1024-65535)", c.Port)
		}
	}
	if !identRe.MatchString(c.Database) || !identRe.MatchString(c.User) {
		return errors.New("pgmanaged: database and user must be simple lowercase identifiers")
	}
	if strings.TrimSpace(c.Home) == "" {
		return errors.New("pgmanaged: home is required")
	}
	return nil
}

func labelArgs(role string) []string {
	return []string{"--label", LabelManaged + "=true", "--label", LabelRole + "=" + role}
}

// loopback reports whether ip is a loopback address (used when verifying bindings).
func loopback(ip string) bool {
	a, err := netip.ParseAddr(ip)
	return err == nil && a.IsLoopback()
}

var instanceRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

// WithInstance returns the configuration of a named instance: every resource
// name and the credentials file carry the suffix, so it can never collide with
// the default instance or another named one.
func (c Config) WithInstance(name string) (Config, error) {
	if name == "" {
		return c, nil
	}
	if !instanceRe.MatchString(name) {
		return c, fmt.Errorf("pgmanaged: instance %q must be lowercase letters, digits and dashes (at most 31 characters)", name)
	}
	c.Instance = name
	c.Container += "-" + name
	c.Volume += "-" + name
	c.Network += "-" + name
	return c, nil
}

func (c Config) suffix() string {
	if c.Instance == "" {
		return ""
	}
	return "-" + c.Instance
}

// SecretsFile is the credentials file of this instance.
func (c Config) SecretsFile() string {
	if c.Instance == "" {
		return SecretsPath(c.Home)
	}
	return filepath.Join(SecretsDir(c.Home), "memory-server"+c.suffix()+".env")
}
