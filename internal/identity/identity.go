// Package identity manages the developer identity: a shared profile in
// .gabitotl/users/<id>.md and the active identity, derived from the OS user
// (GABITOTL_USER overrides). No identity file is kept and nobody is asked who
// they are. Emails are never stored.
package identity

import (
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/procattr"
)

// EnvUser overrides the identity derived from the OS user.
const EnvUser = gabitotl.EnvUser

// ErrNoActive is returned by Active when no id can be derived from the
// environment.
var ErrNoActive = errors.New("no active identity: cannot derive an id from the OS user (set GABITOTL_USER)")

// User is a developer known to the project.
type User struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	DisplayName string `yaml:"display_name,omitempty"`
}

const body = `
# Developer

Known developer of this project. No emails or secrets are stored here.
`

// Slug turns free text into a user ID.
func Slug(s string) string { return gabitotl.Slug(s) }

// New builds a User, deriving the ID from id (or from name when id is empty)
// and the display name from name when absent.
func New(id, name, displayName string) (User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return User{}, errors.New("name is required")
	}
	if id == "" {
		id = name
	}
	slug := Slug(id)
	if slug == "" {
		return User{}, fmt.Errorf("cannot derive a valid id from %q", id)
	}
	if displayName == "" {
		displayName = name
	}
	return User{ID: slug, Name: name, DisplayName: displayName}, nil
}

// Validate checks that the user can be persisted.
func (u User) Validate() error {
	if !gabitotl.ValidID(u.ID) {
		return gabitotl.ErrInvalidID
	}
	if strings.TrimSpace(u.Name) == "" {
		return errors.New("name is required")
	}
	return nil
}

// Save writes the shared profile (users/<id>.md), keeping an existing body.
func Save(root string, u User) error {
	if err := u.Validate(); err != nil {
		return err
	}
	shared, err := gabitotl.NewLayout(root).UserFile(u.ID)
	if err != nil {
		return err
	}
	return gabitotl.WriteDoc(shared, u, keepBody(shared, body))
}

func keepBody(path, def string) string {
	if d, err := gabitotl.ReadDoc(path); err == nil {
		return d.Body
	}
	return def
}

func read(path string) (User, error) {
	d, err := gabitotl.ReadDoc(path)
	if err != nil {
		return User{}, err
	}
	var u User
	if err := d.Decode(&u); err != nil {
		return User{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := u.Validate(); err != nil {
		return User{}, fmt.Errorf("%s: %w", path, err)
	}
	return u, nil
}

// OSUserID derives the active id: GABITOTL_USER when set, else the OS user
// name (see gabitotl.ActiveUserID). The result is a slug; it is empty when
// nothing usable is available.
func OSUserID() string { return gabitotl.ActiveUserID() }

// slugOSName strips a Windows DOMAIN\ prefix and a user@domain suffix, then
// slugs what is left.
func slugOSName(raw string) string { return gabitotl.SlugOSName(raw) }

// Active returns the identity of the OS user (see OSUserID) and silently
// creates its shared profile when the project has none. An existing profile is
// never rewritten. It returns ErrNoActive only when no id can be derived.
func Active(root string) (User, error) {
	id := OSUserID()
	if !gabitotl.ValidID(id) {
		return User{}, ErrNoActive
	}
	u, err := Load(root, id)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return User{}, err // present but unreadable: never overwrite it
	}
	u = User{ID: id, Name: id, DisplayName: id}
	if n := gitName(root); n != "" {
		u.Name, u.DisplayName = n, n
	}
	// Best effort: only inside an initialized project, and a read-only
	// checkout must not make the identity unavailable.
	if gabitotl.Exists(root) {
		_ = Save(root, u)
	}
	return u, nil
}

// Load returns a known user by ID from users/<id>.md.
func Load(root, id string) (User, error) {
	p, err := gabitotl.NewLayout(root).UserFile(id)
	if err != nil {
		return User{}, err
	}
	return read(p)
}

// gitName returns `git config user.name` as seen from dir, or "".
func gitName(dir string) string {
	cmd := procattr.NoWindow(exec.Command("git", "config", "user.name"))
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
