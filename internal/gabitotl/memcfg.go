package gabitotl

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Team memory settings are split in two files so a teammate never inherits the
// facts of another machine (docs/DECISIONS.md D30):
//
//   - .gabitotl/memory.md (committed): mode, provider, backend, project_key and
//     the TEAM-REACHABLE server url, never a loopback address.
//   - .gabitotl/local/users/<id>/memory.md (never committed): role, instance,
//     token (an env reference), postgres_connection and an optional url that
//     overrides the shared one on this machine (a host reaches its own server on
//     127.0.0.1 while the shared file holds the public address).
//
// MemoryConfig is the merged view, so callers keep reading one struct.

// ErrLoopbackTeamURL is returned when the url meant for the shared file points at
// the machine itself: teammates could never reach it.
var ErrLoopbackTeamURL = errors.New("the team url must be reachable by your teammates, not a loopback address (127.0.0.1, ::1, localhost, 0.0.0.0)")

// localMemory is the front matter of the per-person memory.md.
type localMemory struct {
	URL                string `yaml:"url,omitempty"`
	Role               string `yaml:"role,omitempty"`
	Instance           string `yaml:"instance,omitempty"`
	Token              string `yaml:"token,omitempty"`
	PostgresConnection string `yaml:"postgres_connection,omitempty"`
}

// machineKeys are the settings that belong to one machine. A legacy shared
// memory.md may still carry them: they keep working and move on the next write.
var machineKeys = []string{"role", "instance", "token", "postgres_connection"}

// IsLoopbackURL reports whether raw points at the machine itself: localhost,
// 127.0.0.0/8, ::1 or an unspecified address (0.0.0.0, ::).
func IsLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

// MemoryOrigin tells where the memory settings of the active person come from.
type MemoryOrigin struct {
	// SharedURL is the url in the committed memory.md ("" when none).
	SharedURL string
	// LocalPath is the per-person file ("" when no id is available) and HasLocal
	// whether it exists.
	LocalPath string
	HasLocal  bool
	// LegacyFields lists the per-machine keys the committed memory.md still carries.
	LegacyFields []string
}

// InspectMemory reports the origin of the settings; it never fails (an unreadable
// file reads as empty, LoadMemory is what reports it).
func InspectMemory(l Layout, user string) MemoryOrigin {
	var o MemoryOrigin
	if d, err := ReadDoc(l.Memory()); err == nil && d.HasFront {
		var raw map[string]any
		if yaml.Unmarshal(d.Front, &raw) == nil {
			if s, ok := raw["url"].(string); ok {
				o.SharedURL = s
			}
			for _, k := range machineKeys {
				if v, ok := raw[k]; ok && v != nil && v != "" {
					o.LegacyFields = append(o.LegacyFields, k)
				}
			}
		}
	}
	if p, err := l.LocalMemory(user); err == nil {
		o.LocalPath = p
		if _, err := ReadDoc(p); err == nil {
			o.HasLocal = true
		}
	}
	return o
}

// readLocalMemory returns the per-person settings; ok is false when the file does
// not exist (or has no front matter).
func readLocalMemory(l Layout, user string) (loc localMemory, ok bool, err error) {
	p, err := l.LocalMemory(user)
	if err != nil {
		return loc, false, err
	}
	d, err := ReadDoc(p)
	if errors.Is(err, fs.ErrNotExist) {
		return loc, false, nil
	}
	if err != nil {
		return loc, false, err
	}
	if !d.HasFront {
		return loc, false, nil
	}
	if err := d.Decode(&loc); err != nil {
		return loc, false, fmt.Errorf("%s: %w", p, err)
	}
	return loc, true, nil
}

// LoadMemory returns the merged settings of the active person (see ActiveUserID).
func LoadMemory(l Layout) (MemoryConfig, error) { return LoadMemoryFor(l, ActiveUserID()) }

// LoadMemoryFor merges the shared memory.md with the per-person file of user;
// the local value wins per field. A shared file that still carries per-machine
// fields (legacy) is the fallback for those the local file lacks. An empty or
// invalid user reads the shared file alone.
func LoadMemoryFor(l Layout, user string) (MemoryConfig, error) {
	var c MemoryConfig
	if err := readConfig(l.Memory(), &c); err != nil {
		return c, err
	}
	if !ValidID(user) {
		return c, nil
	}
	loc, ok, err := readLocalMemory(l, user)
	if err != nil {
		return c, err
	}
	if !ok {
		return c, nil
	}
	for dst, src := range map[*string]string{
		&c.URL: loc.URL, &c.Role: loc.Role, &c.Instance: loc.Instance, &c.Token: loc.Token, &c.PostgresConnection: loc.PostgresConnection,
	} {
		if src != "" {
			*dst = src
		}
	}
	if err := c.Validate(); err != nil {
		p, _ := l.LocalMemory(user)
		return c, fmt.Errorf("%s: %w", p, err)
	}
	return c, nil
}

// SaveMemory splits c between the shared and the per-person file of the active
// person (see SaveMemoryTeamURL).
func SaveMemory(l Layout, c MemoryConfig) error {
	return SaveMemoryTeamURL(l, ActiveUserID(), c, "")
}

// SaveMemoryFor is SaveMemory for an explicit user.
func SaveMemoryFor(l Layout, user string, c MemoryConfig) error {
	return SaveMemoryTeamURL(l, user, c, "")
}

// SaveMemoryTeamURL writes c, where c.URL is the url this machine uses. teamURL,
// when set, is the address teammates use and goes to the shared file; it must not
// be loopback (ErrLoopbackTeamURL). Without teamURL, a c.URL that is not loopback
// and not this machine's own override is the team url. A loopback c.URL never
// reaches the shared file: it is kept in the per-person file as the override.
//
// Per-machine fields move to local/users/<user>/memory.md and are removed from
// the shared file; the Markdown body and unknown front matter keys of both files
// are preserved. With no usable user id everything stays in the shared file, as
// before the split.
func SaveMemoryTeamURL(l Layout, user string, c MemoryConfig, teamURL string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if teamURL != "" {
		probe := MemoryConfig{Mode: c.Mode, Provider: c.Provider, ProjectKey: c.ProjectKey, URL: teamURL}
		if err := probe.Validate(); err != nil {
			return err
		}
		if IsLoopbackURL(teamURL) {
			return ErrLoopbackTeamURL
		}
	}
	shared := []frontEdit{{"mode", c.Mode}, {"provider", c.Provider}, {"backend", c.Backend}, {"project_key", c.ProjectKey}}
	if !ValidID(user) {
		u := c.URL
		if teamURL != "" {
			u = teamURL
		}
		shared = append(shared, frontEdit{"url", u}, frontEdit{"role", c.Role}, frontEdit{"instance", c.Instance},
			frontEdit{"token", c.Token}, frontEdit{"postgres_connection", c.PostgresConnection})
		return writeFront(l.Memory(), bodyMemory, shared, nil)
	}

	cur, _, err := readLocalMemory(l, user)
	if err != nil {
		return err
	}
	sharedURL := InspectMemory(l, user).SharedURL
	if IsLoopbackURL(sharedURL) {
		sharedURL = "" // legacy: a loopback address in the repository helps nobody
	}
	eff := c.URL
	if teamURL == "" && eff != "" && !IsLoopbackURL(eff) && eff != cur.URL {
		teamURL = eff
	}
	switch {
	case teamURL != "":
		sharedURL = teamURL
	case eff == "":
		sharedURL = ""
	}
	localURL := ""
	if eff != "" && eff != sharedURL {
		localURL = eff
	}

	// Local first: if it fails, the shared file still holds the legacy fields.
	p, err := l.LocalMemory(user)
	if err != nil {
		return err
	}
	local := []frontEdit{{"url", localURL}, {"role", c.Role}, {"instance", c.Instance}, {"token", c.Token}, {"postgres_connection", c.PostgresConnection}}
	_, statErr := ReadDoc(p)
	if statErr == nil || slices.ContainsFunc(local, func(e frontEdit) bool { return e.val != "" }) {
		if _, err := EnsureLocalUserDir(l, user); err != nil {
			return err
		}
		if err := writeFront(p, bodyLocalMemory, local, nil); err != nil {
			return err
		}
	}
	shared = append(shared, frontEdit{"url", sharedURL})
	return writeFront(l.Memory(), bodyMemory, shared, machineKeys)
}

// frontEdit sets one scalar front matter key; an empty value removes the key.
type frontEdit struct{ key, val string }

// writeFront applies edits to the front matter of path and drops the keys in
// drop, keeping the Markdown body, the order and comments of what it does not
// touch, and any key it does not know. A missing file starts from defaultBody.
func writeFront(path, defaultBody string, set []frontEdit, drop []string) error {
	body := defaultBody
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	d, err := ReadDoc(path)
	switch {
	case err == nil:
		body = d.Body
		if d.HasFront && len(bytes.TrimSpace(d.Front)) > 0 {
			var doc yaml.Node
			if err := yaml.Unmarshal(d.Front, &doc); err != nil {
				return fmt.Errorf("%s: invalid front matter: %w", path, err)
			}
			if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
				return fmt.Errorf("%s: front matter is not a mapping", path)
			}
			m = doc.Content[0]
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	for _, k := range drop {
		removeKey(m, k)
	}
	for _, e := range set {
		setKey(m, e.key, e.val)
	}
	var front []byte
	if len(m.Content) > 0 {
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(m); err != nil {
			return fmt.Errorf("render front matter: %w", err)
		}
		if err := enc.Close(); err != nil {
			return err
		}
		front = buf.Bytes()
	}
	return WriteFile(path, Doc{Front: front, HasFront: true, Body: body}.Bytes())
}

func removeKey(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = slices.Delete(m.Content, i, i+2)
			return
		}
	}
}

func setKey(m *yaml.Node, key, val string) {
	if val == "" {
		removeKey(m, key)
		return
	}
	v := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: val}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			v.HeadComment, v.LineComment, v.FootComment = m.Content[i+1].HeadComment, m.Content[i+1].LineComment, m.Content[i+1].FootComment
			m.Content[i+1] = v
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
}

// AwaitsClientSetup reports the state of a teammate right after `git pull`: the
// project uses team memory with a server url a teammate can reach, and nothing
// per person is configured yet. The role then defaults to client and only the
// token variable is missing.
func (c MemoryConfig) AwaitsClientSetup() bool {
	return c.Provider == MemoryMemoryMCP && c.URL != "" && !IsLoopbackURL(c.URL) && c.Role == "" && c.Token == ""
}
