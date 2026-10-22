package desktop

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
)

// osFS implements the file operations that behave the same on every OS.
// ScheduleRemoval is added by the platform file.
type osFS struct{}

// CopyFile writes dst through a temp file in the same directory and renames
// it into place, so a failed copy never leaves a half-written executable.
func (osFS) CopyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	return writeAtomic(dst, in)
}

// WriteFile writes data to dst with the same temp file + rename guarantee.
func (osFS) WriteFile(dst string, data []byte) (int64, error) {
	return writeAtomic(dst, bytes.NewReader(data))
}

func writeAtomic(dst string, r io.Reader) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp*")
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(tmp, r)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o755)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), dst)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return 0, err
	}
	return n, nil
}

func (osFS) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (osFS) RemoveAll(path string) error { return os.RemoveAll(path) }
