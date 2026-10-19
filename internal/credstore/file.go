package credstore

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// FilePath is the secrets file of the user under home.
// It is "" when home is empty or relative: never a path under the working directory.
func FilePath(home string) string {
	if home == "" || !filepath.IsAbs(home) {
		return ""
	}
	return filepath.Join(home, ".gabitotl", "secrets", "vcs-credentials")
}

// File keeps "host=token" lines in a 0600 file inside a 0700 directory. It is the
// adapter for every OS without a native credential API.
type File struct{ Path string }

// NewFile returns the file store of the user whose home folder is home.
func NewFile(home string) *File { return &File{Path: FilePath(home)} }

func (f *File) read() (map[string]string, error) {
	if f.Path == "" {
		return nil, ErrNoHome
	}
	m := map[string]string{}
	fh, err := os.Open(f.Path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m, sc.Err()
}

func (f *File) write(m map[string]string) error {
	if f.Path == "" {
		return ErrNoHome
	}
	dir := filepath.Dir(f.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# GabitoTL Git platform tokens. Mode 0600. Never commit or share.\n")
	for k, v := range m {
		b.WriteString(k + "=" + v + "\n")
	}
	tmp, err := os.CreateTemp(dir, ".vcs-credentials-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // a no-op after the rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, f.Path)
}

// Get implements Store.
func (f *File) Get(host string) (string, bool, error) {
	if err := check(host, "", false); err != nil {
		return "", false, err
	}
	m, err := f.read()
	if err != nil {
		return "", false, err
	}
	t, ok := m[NormalizeHost(host)]
	return t, ok && t != "", nil
}

// Set implements Store.
func (f *File) Set(host, token string) error {
	if err := check(host, token, true); err != nil {
		return err
	}
	m, err := f.read()
	if err != nil {
		return err
	}
	m[NormalizeHost(host)] = strings.TrimSpace(token)
	return f.write(m)
}

// Delete implements Store.
func (f *File) Delete(host string) error {
	if err := check(host, "", false); err != nil {
		return err
	}
	m, err := f.read()
	if err != nil {
		return err
	}
	if _, ok := m[NormalizeHost(host)]; !ok {
		return nil
	}
	delete(m, NormalizeHost(host))
	return f.write(m)
}
