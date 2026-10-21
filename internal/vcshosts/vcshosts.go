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
	"net"
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

// Typed failures, so callers never match English text.
var (
	// ErrInsecureHTTP means an http:// URL for a host that is not loopback: the token would travel in clear text.
	ErrInsecureHTTP = errors.New("vcshosts: the server URL uses plain http")
	// ErrInvalidURL means the server URL (or host) cannot be used.
	ErrInvalidURL = errors.New("vcshosts: the server URL is not valid")
	// ErrHostConflict means the host already names a different server (credentials are
	// keyed by host name without the port, so one host name can hold only one server).
	ErrHostConflict = errors.New("vcshosts: this host name already belongs to a different server")
)

// Entry is one self-hosted server.
type Entry struct {
	Host     string `json:"host"`     // normalized, no port: the credential store key
	Platform string `json:"platform"` // one of Platforms
	URL      string `json:"url"`      // web base URL: scheme, host, optional port and context path
	// SSHHosts are other host names the server's Git remotes use (for example an SSH
	// host that differs from the web host). They only help to recognise a remote: a
	// token is only ever sent to the host of URL.
	SSHHosts []string `json:"sshHosts,omitempty"`
}

// PutOptions tune Put.
type PutOptions struct {
	// Replace lets Put overwrite an entry of the same host that names a different server.
	Replace bool
	// AllowInsecureHTTP accepts an http:// URL for a host that is not loopback.
	AllowInsecureHTTP bool
}

// Registry is the port.
type Registry interface {
	List() ([]Entry, error)
	// Get finds an entry by its host or by one of its SSH host aliases.
	Get(host string) (Entry, bool, error)
	Put(e Entry, o PutOptions) error
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
	return parseBaseURL(raw, false)
}

// ParseBaseURLInsecure is ParseBaseURL that, when allowHTTP is set, also accepts
// http:// for a host that is not loopback (the token then travels in clear text).
func ParseBaseURLInsecure(raw string, allowHTTP bool) (base, host string, err error) {
	return parseBaseURL(raw, allowHTTP)
}

func invalid(msg string) error { return fmt.Errorf("%w: %s", ErrInvalidURL, msg) }

// IsLoopbackHost reports whether host is localhost or a loopback address.
func IsLoopbackHost(host string) bool {
	h := strings.ToLower(strings.Trim(host, "[]"))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func parseBaseURL(raw string, allowHTTP bool) (base, host string, err error) {
	raw = strings.TrimSpace(raw)
	u, perr := url.Parse(raw)
	if perr != nil || raw == "" {
		return "", "", invalid("it cannot be read")
	}
	switch {
	case u.Scheme != "https" && u.Scheme != "http":
		return "", "", invalid("it must start with https://")
	case u.User != nil:
		return "", "", invalid("it must not contain a user or password")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#"):
		return "", "", invalid("it must not contain a query or fragment")
	case u.Hostname() == "":
		return "", "", invalid("it has no host")
	}
	host = credstore.NormalizeHost(u.Hostname())
	if !credstore.ValidHost(host) {
		return "", "", invalid("the host name is not valid")
	}
	if p := u.Port(); p != "" && strings.Trim(p, "0123456789") != "" {
		return "", "", invalid("the port is not valid")
	}
	if u.Scheme == "http" && !allowHTTP && !IsLoopbackHost(u.Hostname()) {
		return "", "", fmt.Errorf("%w: use https:// (plain http sends the token in clear text)", ErrInsecureHTTP)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), host, nil
}

// Normalize validates e and returns it in canonical form (host derived from the URL).
func Normalize(e Entry) (Entry, error) { return normalize(e, false) }

// NormalizeInsecure is Normalize that, when allowHTTP is set, accepts plain http for any host.
func NormalizeInsecure(e Entry, allowHTTP bool) (Entry, error) { return normalize(e, allowHTTP) }

func normalize(e Entry, allowHTTP bool) (Entry, error) {
	if !ValidPlatform(e.Platform) {
		return Entry{}, fmt.Errorf("vcshosts: unknown platform %q (use %s)", e.Platform, strings.Join(Platforms, ", "))
	}
	base, host, err := parseBaseURL(e.URL, allowHTTP)
	if err != nil {
		return Entry{}, err
	}
	if e.Host != "" && credstore.NormalizeHost(e.Host) != host {
		return Entry{}, errors.New("vcshosts: the host does not match the server URL")
	}
	var aliases []string
	for _, a := range e.SSHHosts {
		a = credstore.NormalizeHost(a)
		if !credstore.ValidHost(a) || strings.ContainsAny(a, ":/@") {
			return Entry{}, invalid("the SSH host name is not valid")
		}
		if a != host && !contains(aliases, a) {
			aliases = append(aliases, a)
		}
	}
	sort.Strings(aliases)
	return Entry{Host: host, Platform: e.Platform, URL: base, SSHHosts: aliases}, nil
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

// conflicts reports whether n would take over the host (or an SSH alias) of a
// different server already in es.
func conflicts(es []Entry, n Entry) bool {
	for _, e := range es {
		if e.Host == n.Host {
			if e.Platform != n.Platform || e.URL != n.URL {
				return true
			}
			continue
		}
		if contains(e.SSHHosts, n.Host) || contains(n.SSHHosts, e.Host) {
			return true
		}
		for _, a := range n.SSHHosts {
			if contains(e.SSHHosts, a) {
				return true
			}
		}
	}
	return false
}

// HasConflict reports whether putting n would replace a different server's entry.
func HasConflict(es []Entry, n Entry) bool { return conflicts(es, n) }

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
		if n, err := normalize(e, true); err == nil { // a hand-edited bad entry is skipped, not trusted
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
	serr := tmp.Sync()
	cerr := tmp.Close()
	if werr != nil || serr != nil || cerr != nil {
		os.Remove(name)
		return errors.Join(werr, serr, cerr)
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
	if err != nil {
		return Entry{}, false, err
	}
	e, ok := find(es, host)
	return e, ok, nil
}

// mutate reads the file right before writing it back (so a change made by another
// process in between is not lost) and writes what change returns.
func (f *File) mutate(change func(es []Entry) ([]Entry, error)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	es, err := f.read()
	if err != nil {
		return err
	}
	out, err := change(es)
	if err != nil {
		return err
	}
	return f.write(out)
}

// Put implements Registry: it adds or updates the entry of its host. A host that
// already names a different server is refused (ErrHostConflict) unless o.Replace.
func (f *File) Put(e Entry, o PutOptions) error {
	n, err := normalize(e, o.AllowInsecureHTTP)
	if err != nil {
		return err
	}
	return f.mutate(func(es []Entry) ([]Entry, error) { return put(es, n, o) })
}

// Delete implements Registry; deleting a missing host is not an error.
func (f *File) Delete(host string) error {
	return f.mutate(func(es []Entry) ([]Entry, error) { return remove(es, host), nil })
}

func put(es []Entry, n Entry, o PutOptions) ([]Entry, error) {
	if conflicts(es, n) {
		if !o.Replace {
			return nil, fmt.Errorf("%w: %s", ErrHostConflict, n.Host)
		}
		es = dropClashing(es, n)
	}
	return upsert(es, n), nil
}

// dropClashing removes the entries that n replaces (same host or a shared alias).
func dropClashing(es []Entry, n Entry) []Entry {
	out := es[:0:0]
	for _, e := range es {
		clash := e.Host == n.Host || contains(e.SSHHosts, n.Host) || contains(n.SSHHosts, e.Host)
		for _, a := range n.SSHHosts {
			clash = clash || contains(e.SSHHosts, a)
		}
		if !clash {
			out = append(out, e)
		}
	}
	return out
}

// find matches host against an entry's host or one of its SSH aliases.
func find(es []Entry, host string) (Entry, bool) {
	h := credstore.NormalizeHost(host)
	for _, e := range es {
		if e.Host == h {
			return e, true
		}
	}
	for _, e := range es {
		if contains(e.SSHHosts, h) {
			return e, true
		}
	}
	return Entry{}, false
}

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
	e, ok := find(f.Entries, host)
	return e, ok, nil
}

// Put implements Registry.
func (f *Fake) Put(e Entry, o PutOptions) error {
	n, err := normalize(e, o.AllowInsecureHTTP)
	if err != nil {
		return err
	}
	es, err := put(f.Entries, n, o)
	if err != nil {
		return err
	}
	f.Entries = es
	return nil
}

// Delete implements Registry.
func (f *Fake) Delete(host string) error {
	f.Entries = remove(f.Entries, host)
	return nil
}

// LookupErr returns the entry of host (or of an SSH alias) from r. A nil registry
// or an empty host is "not registered"; an unreadable or corrupt registry is an
// error naming its file, never a silent "not registered".
func LookupErr(r Registry, host string) (Entry, bool, error) {
	if r == nil || host == "" {
		return Entry{}, false, nil
	}
	return r.Get(host)
}

// Lookup is LookupErr for callers that only infer (a read error means "not registered").
func Lookup(r Registry, host string) (Entry, bool) {
	e, ok, err := LookupErr(r, host)
	return e, ok && err == nil
}
