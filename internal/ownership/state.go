package ownership

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
)

// StateVersion is the schema version of state.json.
const StateVersion = 1

// Key is one managed JSON key (or one managed string item of a JSON array).
type Key struct {
	Path     []string        `json:"path"`
	Item     string          `json:"item,omitempty"`     // set for array items
	Previous json.RawMessage `json:"previous,omitempty"` // value the user had before we overwrote it
	Prune    bool            `json:"prune,omitempty"`    // we created the parent, remove it when empty
}

// Entry is the ownership record of one file we touched (spec 38). Runtimes lists
// every adapter that depends on the file, so shared files (Kiro CLI and IDE)
// are only cleaned when the last owner leaves.
type Entry struct {
	Runtimes    []string `json:"runtimes"`
	Path        string   `json:"path"`
	Sections    []string `json:"sections,omitempty"`     // Markdown sections
	Keys        []Key    `json:"managed_keys,omitempty"` // JSON/JSONC keys
	CreatedFile bool     `json:"created_file,omitempty"` // we created the file; delete it when it ends up empty
	Whole       bool     `json:"whole_file,omitempty"`   // we wrote the entire file (a skill); delete it only while its hash equals HashAfter
	Dirs        []string `json:"created_dirs,omitempty"` // directories we created for the file, outermost first; removed when empty
	HashBefore  string   `json:"hash_before"`
	HashAfter   string   `json:"hash_after"`
	Version     string   `json:"version"`
}

// State is ~/.gabitotl/state.json.
type State struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Store reads and writes the global state file under an injected directory
// (production: ~/.gabitotl), so tests never touch the real home.
type Store struct{ Dir string }

func (s Store) path() string { return filepath.Join(s.Dir, "state.json") }

// Load returns the state; a missing file is an empty state.
func (s Store) Load() (State, error) {
	b, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return State{Version: StateVersion}, nil
	}
	if err != nil {
		return State{}, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return State{}, errors.New("ownership: state.json is not valid JSON; refusing to overwrite it")
	}
	return st, nil
}

// Save writes the state atomically.
func (s Store) Save(st State) error {
	st.Version = StateVersion
	slices.SortFunc(st.Entries, func(a, b Entry) int {
		if a.Path != b.Path {
			return compareStr(a.Path, b.Path)
		}
		return 0
	})
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, ".state-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path())
}

func compareStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Find returns the entry for path.
func (st State) Find(path string) (int, bool) {
	for i, e := range st.Entries {
		if e.Path == path {
			return i, true
		}
	}
	return -1, false
}

// ForRuntime lists the entries a runtime owns (alone or shared).
func (st State) ForRuntime(runtime string) []Entry {
	var out []Entry
	for _, e := range st.Entries {
		if slices.Contains(e.Runtimes, runtime) {
			out = append(out, e)
		}
	}
	return out
}

// Upsert merges e into the state: runtimes, sections and keys are unioned,
// HashBefore and CreatedFile keep their first recorded value.
func (st *State) Upsert(e Entry) {
	i, ok := st.Find(e.Path)
	if !ok {
		st.Entries = append(st.Entries, e)
		return
	}
	cur := &st.Entries[i]
	for _, r := range e.Runtimes {
		if !slices.Contains(cur.Runtimes, r) {
			cur.Runtimes = append(cur.Runtimes, r)
		}
	}
	for _, sec := range e.Sections {
		if !slices.Contains(cur.Sections, sec) {
			cur.Sections = append(cur.Sections, sec)
		}
	}
	for _, k := range e.Keys {
		if !containsKey(cur.Keys, k) {
			cur.Keys = append(cur.Keys, k)
		}
	}
	for _, d := range e.Dirs {
		if !slices.Contains(cur.Dirs, d) {
			cur.Dirs = append(cur.Dirs, d)
		}
	}
	cur.Whole = cur.Whole || e.Whole
	cur.HashAfter, cur.Version = e.HashAfter, e.Version
}

func containsKey(keys []Key, k Key) bool {
	return slices.ContainsFunc(keys, func(o Key) bool { return slices.Equal(o.Path, k.Path) && o.Item == k.Item })
}

// Hash returns the hex SHA-256 of data, or "" for a file that did not exist (nil).
func Hash(data []byte) string {
	if data == nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
