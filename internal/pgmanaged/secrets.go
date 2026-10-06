package pgmanaged

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SecretsDir is where credentials live: <home>/.gabitotl/secrets, mode 0700. It
// is outside every project, so it can never be committed with .gabitotl/.
func SecretsDir(home string) string { return filepath.Join(home, ".gabitotl", "secrets") }

// SecretsPath is the credentials file of the managed instance.
func SecretsPath(home string) string { return filepath.Join(SecretsDir(home), "memory-server.env") }

// BackupsDir holds database dumps.
func BackupsDir(home string) string {
	return filepath.Join(home, ".gabitotl", "backups", "memory-postgres")
}

// Credentials are the generated values of one managed instance.
type Credentials struct {
	Password string
	URL      string
}

// randomPassword returns 256 bits of hex (no characters that need URL escaping).
func randomPassword(r io.Reader) (string, error) {
	var b [32]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return "", fmt.Errorf("pgmanaged: cannot generate a password: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// DatabaseURL builds the loopback connection string for the instance.
func (c Config) DatabaseURL(password string, port int) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", c.User, password, Bind, port, c.Database)
}

// WriteCredentials stores the credentials atomically with mode 0600.
func WriteCredentials(home string, c Credentials) error {
	dir := SecretsDir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	body := "# GabitoTL managed memory PostgreSQL. Generated file, mode 0600.\n" +
		"# Never commit it, paste it in a chat or copy it into a project.\n" +
		EnvPassword + "=" + c.Password + "\n" + EnvDatabaseURL + "=" + c.URL + "\n"
	tmp, err := os.CreateTemp(dir, ".memory-server-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), SecretsPath(home))
}

// ReadEnvFile parses KEY=VALUE lines (comments and blanks ignored).
func ReadEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out, sc.Err()
}

// ReadCredentials loads the credentials file. A missing file is ErrNoCredentials.
func ReadCredentials(home string) (Credentials, error) {
	m, err := ReadEnvFile(SecretsPath(home))
	if errors.Is(err, fs.ErrNotExist) {
		return Credentials{}, ErrNoCredentials
	}
	if err != nil {
		return Credentials{}, err
	}
	c := Credentials{Password: m[EnvPassword], URL: m[EnvDatabaseURL]}
	if c.Password == "" || c.URL == "" {
		return Credentials{}, fmt.Errorf("%w: %s does not hold %s and %s", ErrNoCredentials, SecretsPath(home), EnvPassword, EnvDatabaseURL)
	}
	return c, nil
}

// LookupSecret resolves a variable the way `memory serve` does: the process
// environment first, then the managed credentials file. Only the two variables
// the file defines are ever read from it.
func LookupSecret(name, home string, getenv func(string) string) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	if v := getenv(name); v != "" {
		return v
	}
	if name != EnvDatabaseURL && name != EnvPassword {
		return ""
	}
	if m, err := ReadEnvFile(SecretsPath(home)); err == nil {
		return m[name]
	}
	return ""
}
