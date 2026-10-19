package route

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// state is manager.json. Root is the folder last worked on; Projects are the
// folders the person added (older files have no list, which reads as empty).
type state struct {
	Root     string   `json:"root"`
	Projects []string `json:"projects,omitempty"`
}

func load(path string) state {
	var s state
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func save(path string, s state) error {
	if path == "" {
		return errors.New("no state file")
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// LoadRoot reads the project folder the Manager last worked on; "" when none
// was saved or the file is unreadable (the Manager then asks for one).
func LoadRoot(path string) string { return load(path).Root }

// SaveRoot remembers the project folder across launches, keeping the list.
func SaveRoot(path, root string) error {
	s := load(path)
	s.Root = root
	return save(path, s)
}

// LoadProjects lists the remembered project folders, plus the last root.
func LoadProjects(path string) []string {
	s := load(path)
	out := append([]string(nil), s.Projects...)
	if s.Root != "" {
		out = append(out, s.Root)
	}
	return out
}

// AddProject remembers a project folder (no duplicates).
func AddProject(path, dir string) error {
	s := load(path)
	for _, p := range s.Projects {
		if p == dir {
			return nil
		}
	}
	s.Projects = append(s.Projects, dir)
	return save(path, s)
}

// ForgetProject drops a folder from the list (and the last root when equal).
func ForgetProject(path, dir string) error {
	s := load(path)
	kept := s.Projects[:0]
	for _, p := range s.Projects {
		if p != dir {
			kept = append(kept, p)
		}
	}
	s.Projects = kept
	if s.Root == dir {
		s.Root = ""
	}
	return save(path, s)
}
