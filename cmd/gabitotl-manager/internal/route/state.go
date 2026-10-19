package route

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

type state struct {
	Root string `json:"root"`
}

// LoadRoot reads the project folder the Manager last worked on; "" when none
// was saved or the file is unreadable (the Manager then asks for one).
func LoadRoot(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var s state
	if json.Unmarshal(b, &s) != nil {
		return ""
	}
	return s.Root
}

// SaveRoot remembers the project folder across launches.
func SaveRoot(path, root string) error {
	if path == "" {
		return errors.New("no state file")
	}
	b, err := json.Marshal(state{Root: root})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
