// Package identity manages the developer identity: a shared profile in
// .gabitotl/users/<id>.md and the active identity in .gabitotl/local/user.md.
// Emails are never stored.
package identity

import (
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// ErrNoActive is returned by Active when no local identity is set.
var ErrNoActive = errors.New("no active identity: .gabitotl/local/user.md is missing")

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

const localBody = `
# Active identity

Local only (not shared through Git). Identifies who is working in this checkout.
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

// Save writes the shared profile (users/<id>.md) and makes the user the
// active identity (local/user.md).
func Save(root string, u User) error {
	if err := u.Validate(); err != nil {
		return err
	}
	l := gabitotl.NewLayout(root)
	shared, err := l.UserFile(u.ID)
	if err != nil {
		return err
	}
	if err := gabitotl.WriteDoc(shared, u, keepBody(shared, body)); err != nil {
		return err
	}
	return gabitotl.WriteDoc(l.LocalUser(), u, keepBody(l.LocalUser(), localBody))
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

// Active returns the identity in local/user.md, or ErrNoActive.
func Active(root string) (User, error) {
	u, err := read(gabitotl.NewLayout(root).LocalUser())
	if errors.Is(err, fs.ErrNotExist) {
		return User{}, ErrNoActive
	}
	return u, err
}

// Load returns a known user by ID from users/<id>.md.
func Load(root, id string) (User, error) {
	p, err := gabitotl.NewLayout(root).UserFile(id)
	if err != nil {
		return User{}, err
	}
	return read(p)
}

// SuggestFromGit proposes an identity from `git config user.name`. It is only
// a suggestion for the user to confirm; it returns ok=false when git has no name.
func SuggestFromGit() (u User, ok bool) {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err != nil {
		return User{}, false
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return User{}, false
	}
	u, err = New("", name, "")
	return u, err == nil
}
