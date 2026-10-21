// Package vcshosts is the registry of self-hosted Git servers the person uses
// (Bitbucket Data Center, GitHub Enterprise, self-managed GitLab). It is
// non-secret: a host name, its platform and the web base URL where the HTTP API
// lives. An SSH remote does not say that (ssh://git@host:7999/... is not the web
// port), so the URL always comes from here. Tokens stay in internal/credstore,
// keyed by the same host name (docs/DECISIONS.md D60).
package vcshosts

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/winnstorm/gabitotl/internal/credstore"
)

// Platforms a registry entry may name.
const (
	BitbucketServer = "bitbucket-server"
	GitHub          = "github"
	GitLab          = "gitlab"
)

// Platforms lists the accepted platform values.
var Platforms = []string{BitbucketServer, GitHub, GitLab}

// ErrNoHome means the home folder is empty or relative, so no registry file can be placed safely.
var ErrNoHome = errors.New("vcshosts: the home folder is unknown or not absolute")

// Entry is one self-hosted server.
type Entry struct {
	Host     string `json:"host"`     // normalized, no port: the credential store key
	Platform string `json:"platform"` // one of Platforms
	URL      string `json:"url"`      // web base URL: scheme, host, optional port and context path
}

// Registry is the port.
type Registry interface {
	List() ([]Entry, error)
	Get(host string) (Entry, bool, error)
	Put(e Entry) error
	Delete(host string) error
}

// ValidPlatform reports whether p is an accepted platform.
func ValidPlatform(p string) bool {
	for _, v := range Platforms {
		if v == p {
			return true
		}
	}
	return false
}

// ParseBaseURL validates a server web URL and returns it normalized (no trailing
// slash) with its host name (no port). Only http and https are accepted; userinfo,
// a query and a fragment are refused (a URL must never carry a credential).
func ParseBaseURL(raw string) (base, host string, err error) {
	raw = strings.TrimSpace(raw)
	u, perr := url.Parse(raw)
	if perr != nil || raw == "" {
		return "", "", errors.New("vcshosts: the server URL is not valid")
	}
	switch {
	case u.Scheme != "https" && u.Scheme != "http":
		return "", "", errors.New("vcshosts: the server URL must start with https:// (or http://)")
	case u.User != nil:
		return "", "", errors.New("vcshosts: the server URL must not contain a user or password")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#"):
		return "", "", errors.New("vcshosts: the server URL must not contain a query or fragment")
	case u.Hostname() == "":
		return "", "", errors.New("vcshosts: the server URL has no host")
	}
	host = credstore.NormalizeHost(u.Hostname())
	if !credstore.ValidHost(host) {
		return "", "", errors.New("vcshosts: the server host name is not valid")
	}
	if p := u.Port(); p != "" && strings.Trim(p, "0123456789") != "" {
		return "", "", errors.New("vcshosts: the server port is not valid")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), host, nil
}

// Normalize validates e and returns it in canonical form (host derived from the URL).
func Normalize(e Entry) (Entry, error) {
	if !ValidPlatform(e.Platform) {
		return Entry{}, fmt.Errorf("vcshosts: unknown platform %q (use %s)", e.Platform, strings.Join(Platforms, ", "))
	}
	base, host, err := ParseBaseURL(e.URL)
	if err != nil {
		return Entry{}, err
	}
	if e.Host != "" && credstore.NormalizeHost(e.Host) != host {
		return Entry{}, errors.New("vcshosts: the host does not match the server URL")
	}
	return Entry{Host: host, Platform: e.Platform, URL: base}, nil
}

// FilePath is the registry file under home; "" when home is empty or relative.
func FilePath(home string) string {
	if home == "" || !filepath.IsAbs(home) {
		return ""
	}
	return filepath.Join(home, ".gabitotl", "vcs-hosts.json")
}

// File keeps the registry in one small JSON file, replaced atomically on write.
type File struct {
	Path string
	mu   sync.Mutex
}

// NewFile returns the registry of the user whose home folder is home.
func NewFile(home string) *File { return &File{Path: FilePath(home)} }

type fileDoc struct {
	Hosts []Entry `json:"hosts"`
}

func (f *File) read() ([]Entry, error) {
	if f.Path == "" {
		return nil, ErrNoHome
	}
	raw, err := os.ReadFile(f.Path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc fileDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("vcshosts: %s is not valid JSON: %w", f.Path, err)
	}
	out := make([]Entry, 0, len(doc.Hosts))
	for _, e := range doc.Hosts {
		if n, err := Normalize(e); err == nil { // a hand-edited bad entry is skipped, not trusted
			out = append(out, n)
		}
	}
	sortEntries(out)
	return out, nil
}

func sortEntries(es []Entry) { sort.Slice(es, func(i, j int) bool { return es[i].Host < es[j].Host }) }

func (f *File) write(es []Entry) error {
	sortEntries(es)
	raw, err := json.MarshalIndent(fileDoc{Hosts: es}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(f.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".vcs-hosts-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(append(raw, '\n'))
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(name)
		return errors.Join(werr, cerr)
	}
	if err := os.Rename(name, f.Path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// List implements Registry.
func (f *File) List() ([]Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.read()
}

// Get implements Registry.
func (f *File) Get(host string) (Entry, bool, error) {
	es, err := f.List()
	return find(es, host), found(es, host), err
}

// Put implements Registry: it adds or replaces the entry of its host.
func (f *File) Put(e Entry) error {
	n, err := Normalize(e)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	es, err := f.read()
	if err != nil {
		return err
	}
	return f.write(upsert(es, n))
}

// Delete implements Registry; deleting a missing host is not an error.
func (f *File) Delete(host string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	es, err := f.read()
	if err != nil {
		return err
	}
	return f.write(remove(es, host))
}

func find(es []Entry, host string) Entry {
	h := credstore.NormalizeHost(host)
	for _, e := range es {
		if e.Host == h {
			return e
		}
	}
	return Entry{}
}

func found(es []Entry, host string) bool { return find(es, host).Host != "" }

func upsert(es []Entry, n Entry) []Entry {
	es = remove(es, n.Host)
	return append(es, n)
}

func remove(es []Entry, host string) []Entry {
	h := credstore.NormalizeHost(host)
	out := es[:0:0]
	for _, e := range es {
		if e.Host != h {
			out = append(out, e)
		}
	}
	return out
}

// Fake is an in-memory Registry for tests.
type Fake struct{ Entries []Entry }

// List implements Registry.
func (f *Fake) List() ([]Entry, error) { return append([]Entry(nil), f.Entries...), nil }

// Get implements Registry.
func (f *Fake) Get(host string) (Entry, bool, error) {
	return find(f.Entries, host), found(f.Entries, host), nil
}

// Put implements Registry.
func (f *Fake) Put(e Entry) error {
	n, err := Normalize(e)
	if err != nil {
		return err
	}
	f.Entries = upsert(f.Entries, n)
	return nil
}

// Delete implements Registry.
func (f *Fake) Delete(host string) error {
	f.Entries = remove(f.Entries, host)
	return nil
}

// Lookup returns the entry of host from r, ignoring a nil registry and read errors.
func Lookup(r Registry, host string) (Entry, bool) {
	if r == nil || host == "" {
		return Entry{}, false
	}
	e, ok, err := r.Get(host)
	return e, ok && err == nil
}
