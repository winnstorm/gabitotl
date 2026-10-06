package coordination

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// Task statuses.
const (
	StatusInProgress = "in-progress"
	StatusBlocked    = "blocked"
	StatusPaused     = "paused"
	StatusDone       = "done"
)

// ActiveWork is the front matter of .gabitotl/work/<user>/<task>.md (spec 29)
// plus the areas and files the task touches, used for soft locks.
type ActiveWork struct {
	ID     string   `yaml:"id"`
	Owner  string   `yaml:"owner"`
	Status string   `yaml:"status"`
	Branch string   `yaml:"branch,omitempty"`
	Areas  []string `yaml:"areas,omitempty"`
	Files  []string `yaml:"files,omitempty"`
}

// Active reports whether the task counts for overlap detection.
func (a ActiveWork) Active() bool { return a.Status == StatusInProgress || a.Status == StatusBlocked }

// TaskState is the Markdown body of a task file. The section headings are the
// file format of spec section 29 and are intentionally kept in Spanish.
type TaskState struct {
	Objective string
	Done      []string // Completado
	Todo      []string // Falta
	Next      string   // Próximo paso
}

// Task is a task file: ActiveWork front matter plus TaskState body.
type Task struct {
	ActiveWork
	State TaskState
}

// Headings of the task file body (spec 29).
const (
	headObjective = "# Objetivo"
	headDone      = "## Completado"
	headTodo      = "## Falta"
	headNext      = "## Próximo paso"
)

func (a ActiveWork) validate() error {
	if !gabitotl.ValidID(a.ID) || !gabitotl.ValidID(a.Owner) {
		return gabitotl.ErrInvalidID
	}
	switch a.Status {
	case StatusInProgress, StatusBlocked, StatusPaused, StatusDone:
		return nil
	}
	return fmt.Errorf("status: %q is not one of in-progress, blocked, paused, done", a.Status)
}

// RenderBody renders the task body.
func (s TaskState) RenderBody() string {
	var b strings.Builder
	list := func(items []string) {
		for _, it := range items {
			fmt.Fprintf(&b, "- %s\n", it)
		}
	}
	fmt.Fprintf(&b, "\n%s\n\n%s\n\n%s\n\n", headObjective, strings.TrimSpace(s.Objective), headDone)
	list(s.Done)
	fmt.Fprintf(&b, "\n%s\n\n", headTodo)
	list(s.Todo)
	fmt.Fprintf(&b, "\n%s\n\n%s\n", headNext, strings.TrimSpace(s.Next))
	return b.String()
}

// ParseBody reads a task body written by RenderBody (or by hand in the same shape).
func ParseBody(body string) TaskState {
	var st TaskState
	var section string
	var text []string
	flush := func() {
		joined := strings.TrimSpace(strings.Join(text, "\n"))
		switch section {
		case headObjective:
			st.Objective = joined
		case headNext:
			st.Next = joined
		}
		text = nil
	}
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		switch t {
		case headObjective, headDone, headTodo, headNext:
			flush()
			section = t
			continue
		}
		switch section {
		case headDone, headTodo:
			if item, ok := strings.CutPrefix(t, "- "); ok && strings.TrimSpace(item) != "" {
				if section == headDone {
					st.Done = append(st.Done, strings.TrimSpace(item))
				} else {
					st.Todo = append(st.Todo, strings.TrimSpace(item))
				}
			}
		default:
			text = append(text, line)
		}
	}
	flush()
	return st
}

// WriteTask saves a task file for its owner.
func WriteTask(layout gabitotl.Layout, t Task) error {
	if err := t.validate(); err != nil {
		return err
	}
	path, err := layout.WorkFile(t.Owner, t.ID)
	if err != nil {
		return err
	}
	return gabitotl.WriteDoc(path, t.ActiveWork, t.State.RenderBody())
}

// ReadTask loads one task file.
func ReadTask(layout gabitotl.Layout, user, task string) (Task, error) {
	path, err := layout.WorkFile(user, task)
	if err != nil {
		return Task{}, err
	}
	return readTaskFile(path)
}

func readTaskFile(path string) (Task, error) {
	d, err := gabitotl.ReadDoc(path)
	if err != nil {
		return Task{}, err
	}
	var a ActiveWork
	if err := d.Decode(&a); err != nil {
		return Task{}, fmt.Errorf("%s: %w", path, err)
	}
	return Task{ActiveWork: a, State: ParseBody(d.Body)}, nil
}

// ListTasks returns every task file of every user (or only user's when set),
// sorted by owner then id. Unreadable files are skipped.
func ListTasks(layout gabitotl.Layout, user string) ([]Task, error) {
	var dirs []string
	if user != "" {
		d, err := layout.WorkDir(user)
		if err != nil {
			return nil, err
		}
		dirs = []string{d}
	} else {
		entries, err := os.ReadDir(layout.Work())
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() && gabitotl.ValidID(e.Name()) {
				dirs = append(dirs, filepath.Join(layout.Work(), e.Name()))
			}
		}
	}
	var out []Task
	for _, d := range dirs {
		files, err := os.ReadDir(d)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, f := range files {
			if f.IsDir() || filepath.Ext(f.Name()) != ".md" {
				continue
			}
			if t, err := readTaskFile(filepath.Join(d, f.Name())); err == nil {
				out = append(out, t)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Owner != out[j].Owner {
			return out[i].Owner < out[j].Owner
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
