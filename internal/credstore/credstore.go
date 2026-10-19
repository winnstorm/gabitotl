// Package credstore keeps the Git platform tokens of the user, keyed by platform
// host (github.com, gitlab.com, bitbucket.org or a custom host). The port is
// Store; the adapters are the Windows Credential Manager (store_windows.go), a
// 0600 file elsewhere (file.go) and an in-memory fake for tests. A token is never
// logged, printed or written to a Markdown file (docs/DECISIONS.md D56).
package credstore

import (
	"errors"
	"strings"
)

// TargetPrefix names every credential this program owns in the OS store. The full
// target is TargetPrefix + the lower-case host, for example "gabitotl:vcs:github.com".
const TargetPrefix = "gabitotl:vcs:"

// Store is the credential port.
type Store interface {
	// Get returns the token of host; ok is false when none is stored.
	Get(host string) (token string, ok bool, err error)
	// Set stores or replaces the token of host.
	Set(host, token string) error
	// Delete removes the token of host; deleting a missing one is not an error.
	Delete(host string) error
}

// Target is the credential name of host in the OS store.
func Target(host string) string { return TargetPrefix + NormalizeHost(host) }

// NormalizeHost lower-cases and trims host.
func NormalizeHost(host string) string { return strings.ToLower(strings.TrimSpace(host)) }

// ValidHost accepts a plain host name (letters, digits, dot, dash, optional :port).
func ValidHost(host string) bool {
	h := NormalizeHost(host)
	if h == "" || len(h) > 253 {
		return false
	}
	for _, r := range h {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-', r == ':':
		default:
			return false
		}
	}
	return !strings.HasPrefix(h, ".") && !strings.HasPrefix(h, "-")
}

// ValidToken accepts a single non-empty line.
func ValidToken(token string) bool {
	return strings.TrimSpace(token) != "" && !strings.ContainsAny(token, "\r\n\x00")
}

func check(host, token string, needToken bool) error {
	if !ValidHost(host) {
		return errors.New("credstore: invalid host")
	}
	if needToken && !ValidToken(token) {
		return errors.New("credstore: the token must be a single non-empty line")
	}
	return nil
}

// Fake is an in-memory Store for tests.
type Fake struct{ M map[string]string }

// NewFake returns an empty Fake.
func NewFake() *Fake { return &Fake{M: map[string]string{}} }

// Get implements Store.
func (f *Fake) Get(host string) (string, bool, error) {
	if err := check(host, "", false); err != nil {
		return "", false, err
	}
	t, ok := f.M[NormalizeHost(host)]
	return t, ok, nil
}

// Set implements Store.
func (f *Fake) Set(host, token string) error {
	if err := check(host, token, true); err != nil {
		return err
	}
	f.M[NormalizeHost(host)] = strings.TrimSpace(token)
	return nil
}

// Delete implements Store.
func (f *Fake) Delete(host string) error {
	if err := check(host, "", false); err != nil {
		return err
	}
	delete(f.M, NormalizeHost(host))
	return nil
}
