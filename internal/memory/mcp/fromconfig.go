package mcp

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
)

// ClientSecretsPath is the 0600 file where a developer may keep the token of
// the team server (KEY=VALUE lines), so runtimes that start the bridge without
// the user's shell environment can still authenticate. Outside every project.
func ClientSecretsPath(home string) string {
	return filepath.Join(home, ".gabitotl", "secrets", "memory-client.env")
}

// Resolver looks variables up the way every memory client does: the process
// environment first, then the client secrets file.
type Resolver struct {
	Getenv func(string) string
	Home   string
}

// Lookup returns the value of name, or "".
func (r Resolver) Lookup(name string) string {
	getenv := r.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if v := getenv(name); v != "" {
		return v
	}
	if r.Home == "" {
		return ""
	}
	f, err := os.Open(ClientSecretsPath(r.Home))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == name {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// WriteClientSecret stores NAME=VALUE in the client secrets file (mode 0600,
// directory 0700), replacing any previous value of NAME and keeping the others.
func WriteClientSecret(home, name, value string) error {
	if !envRefRe.MatchString("env:" + name) {
		return fmt.Errorf("%q is not a valid variable name", name)
	}
	if strings.ContainsAny(value, "\r\n") || value == "" {
		return errors.New("the secret must be a single non-empty line")
	}
	path := ClientSecretsPath(home)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	var keep []string
	if b, err := os.ReadFile(path); err == nil {
		for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
			if k, _, ok := strings.Cut(l, "="); ok && strings.TrimSpace(k) == name {
				continue
			}
			if strings.TrimSpace(l) != "" {
				keep = append(keep, l)
			}
		}
	}
	if len(keep) == 0 || !strings.HasPrefix(keep[0], "#") {
		keep = append([]string{"# GabitoTL team memory client secrets. Mode 0600. Never commit or share."}, keep...)
	}
	keep = append(keep, name+"="+value)
	tmp, err := os.CreateTemp(dir, ".memory-client-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(strings.Join(keep, "\n") + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// NewFromConfig builds the memory-mcp provider for memory.md: a streamable-HTTP
// client of cfg.URL authenticated with the token that cfg.Token (env:NAME)
// points at. Nothing is resolved until a call is made.
func NewFromConfig(cfg gabitotl.MemoryConfig, r Resolver) (memory.Provider, error) {
	if cfg.PostgresConnection != "" && !envRefRe.MatchString(cfg.PostgresConnection) {
		return nil, fmt.Errorf("memory-mcp: postgres_connection must be an env:NAME reference")
	}
	if cfg.URL == "" {
		return nil, errors.New("memory-mcp: .gabitotl/memory.md has no url (the team server endpoint, e.g. https://memoria.example.com/mcp)")
	}
	if cfg.Token == "" {
		return nil, errors.New("memory-mcp: .gabitotl/memory.md has no token reference (token: env:NAME)")
	}
	tr, err := NewHTTPTransport(HTTPOptions{URL: cfg.URL, TokenRef: cfg.Token, Getenv: r.Lookup})
	if err != nil {
		return nil, fmt.Errorf("memory-mcp: %w", err)
	}
	return New(Options{Transport: tr, ConnectionRef: cfg.Token, Project: cfg.ProjectKey, Getenv: r.Lookup})
}

// FromConfig is NewFromConfig with the process environment and the user home.
func FromConfig(cfg gabitotl.MemoryConfig) (memory.Provider, error) {
	home, _ := os.UserHomeDir()
	return NewFromConfig(cfg, Resolver{Home: home})
}
