// Package gabitotl implements the project layer: the .gabitotl/ folder
// layout, its Markdown+front matter files, secret guard and project detection.
package gabitotl

import "path/filepath"

// DirName is the project folder managed by GabitoTL.
const DirName = ".gabitotl"

// Layout resolves every path inside a project's .gabitotl/ folder.
type Layout struct {
	Root string // project root (the directory that contains .gabitotl/)
}

// NewLayout returns the layout for a project root.
func NewLayout(root string) Layout { return Layout{Root: root} }

func (l Layout) join(parts ...string) string {
	return filepath.Join(append([]string{l.Root, DirName}, parts...)...)
}

// Dir is the .gabitotl directory.
func (l Layout) Dir() string { return l.join() }

// Shared configuration files (committed to Git).
func (l Layout) Project() string  { return l.join("project.md") }
func (l Layout) Memory() string   { return l.join("memory.md") }
func (l Layout) Runtime() string  { return l.join("runtime.md") }
func (l Layout) VCS() string      { return l.join("vcs.md") }
func (l Layout) Workflow() string { return l.join("workflow.md") }
func (l Layout) Imports() string  { return l.join("imports.md") }

// Users is the directory of known developers (committed).
func (l Layout) Users() string { return l.join("users") }

// UserFile is the shared profile of a developer.
func (l Layout) UserFile(id string) (string, error) {
	if err := checkID(id); err != nil {
		return "", err
	}
	return l.join("users", id+".md"), nil
}

// Work is the root of per-user task state (committed).
func (l Layout) Work() string { return l.join("work") }

// WorkDir is the task directory of one user.
func (l Layout) WorkDir(user string) (string, error) {
	if err := checkID(user); err != nil {
		return "", err
	}
	return l.join("work", user), nil
}

// WorkFile is the state file of one task.
func (l Layout) WorkFile(user, task string) (string, error) {
	if err := checkID(user); err != nil {
		return "", err
	}
	if err := checkID(task); err != nil {
		return "", err
	}
	return l.join("work", user, task+".md"), nil
}

// Local-only files (never committed).
func (l Layout) Local() string        { return l.join("local") }
func (l Layout) LocalUser() string    { return l.join("local", "user.md") }
func (l Layout) LocalRuntime() string { return l.join("local", "runtime.md") }
func (l Layout) LocalSession() string { return l.join("local", "session.md") }

// Bak is the backup root; BakUser is one user's backup directory.
func (l Layout) Bak() string { return l.join(".bak") }

// BakUser is the backup directory of one user.
func (l Layout) BakUser(user string) (string, error) {
	if err := checkID(user); err != nil {
		return "", err
	}
	return l.join(".bak", user), nil
}

// Tmp is the scratch directory (never committed).
func (l Layout) Tmp() string { return l.join("tmp") }

// Gitignore is .gabitotl/.gitignore.
func (l Layout) Gitignore() string { return l.join(".gitignore") }

// GitignoreEntries are the paths that must never be committed.
var GitignoreEntries = []string{"local/", ".bak/", "tmp/"}
