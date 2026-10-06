package gabitotl

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFile writes data to path after the secret and branding guards pass. It replaces
// the file atomically (temp file + rename). Every .gabitotl write goes through here.
func WriteFile(path string, data []byte) error {
	if err := ValidateNoSecrets(data); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := ValidateNoBranding(data); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gabitotl-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// WriteFileIfMissing creates path only when it does not exist.
// It reports whether the file was created and never overwrites.
func WriteFileIfMissing(path string, data []byte) (bool, error) {
	if err := ValidateNoSecrets(data); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if err := ValidateNoBranding(data); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return false, err
	}
	return true, f.Close()
}

// WriteDoc renders front + body and writes it, replacing any existing file.
func WriteDoc(path string, front any, body string) error {
	data, err := RenderDoc(front, body)
	if err != nil {
		return err
	}
	return WriteFile(path, data)
}

// ReadDoc reads and parses a Markdown document.
func ReadDoc(path string) (Doc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Doc{}, err
	}
	d, err := ParseDoc(data)
	if err != nil {
		return Doc{}, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}

// readConfig decodes the front matter of path into v (pre-filled with defaults).
func readConfig(path string, v interface{ Validate() error }) error {
	d, err := ReadDoc(path)
	if err != nil {
		return err
	}
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := v.Validate(); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// saveConfig validates v and writes it, keeping the existing Markdown body
// when the file already exists and defaultBody otherwise.
func saveConfig(path string, v interface{ Validate() error }, defaultBody string) error {
	if err := v.Validate(); err != nil {
		return err
	}
	body := defaultBody
	if d, err := ReadDoc(path); err == nil {
		body = d.Body
	}
	return WriteDoc(path, v, body)
}
