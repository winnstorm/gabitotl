package coordination

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"gopkg.in/yaml.v3"
)

// LockFile is the session lock inside .gabitotl/local/ (never committed).
const LockFile = "workdir.lock"

// Holder identifies the session that owns a working directory.
type Holder struct {
	PID     int       `yaml:"pid"`
	User    string    `yaml:"user"`
	Started time.Time `yaml:"started"`
}

// BusyError reports that another live session already uses this working
// directory. Its message tells the second user what to do.
type BusyError struct {
	Holder Holder
	Dir    string
}

func (e *BusyError) Error() string {
	return fmt.Sprintf("this working directory is already in use by %q (pid %d, since %s). "+
		"Two people must not work in the same directory: create your own worktree with "+
		"`gabitotl worktree add --user <you> --task <task> --branch <branch>` and work there",
		e.Holder.User, e.Holder.PID, e.Holder.Started.Local().Format("2006-01-02 15:04"))
}

// ErrBusy matches any BusyError with errors.Is.
var ErrBusy = errors.New("coordination: working directory is busy")

func (e *BusyError) Is(target error) bool { return target == ErrBusy }

// Lock is a held session lock.
type Lock struct {
	path   string
	holder Holder
}

// Holder returns the lock owner.
func (l *Lock) Holder() Holder { return l.holder }

// pidAlive is replaced in tests. See lock_unix.go for the real check.
var pidAlive = processAlive

// Acquire takes the session lock of a working directory for user. A lock whose
// process is gone is stale and is replaced. The same process may re-acquire.
func Acquire(layout gabitotl.Layout, user string, now time.Time) (*Lock, error) {
	if !gabitotl.ValidID(user) {
		return nil, gabitotl.ErrInvalidID
	}
	path := filepath.Join(layout.Local(), LockFile)
	if err := os.MkdirAll(layout.Local(), 0o755); err != nil {
		return nil, err
	}
	mine := Holder{PID: os.Getpid(), User: user, Started: now.UTC()}
	data, err := yaml.Marshal(mine)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_, werr := f.Write(data)
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				_ = os.Remove(path)
				return nil, werr
			}
			return &Lock{path: path, holder: mine}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		cur, rerr := readHolder(path)
		if rerr == nil && cur.PID == mine.PID {
			return &Lock{path: path, holder: cur}, nil // same process
		}
		if rerr == nil && pidAlive(cur.PID) {
			return nil, &BusyError{Holder: cur, Dir: layout.Root}
		}
		// Stale or unreadable: remove and retry.
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return nil, errors.New("coordination: could not acquire the session lock")
}

func readHolder(path string) (Holder, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Holder{}, err
	}
	var h Holder
	if err := yaml.Unmarshal(b, &h); err != nil {
		return Holder{}, err
	}
	if h.PID <= 0 {
		return Holder{}, errors.New("invalid lock file")
	}
	return h, nil
}

// Inspect reports the current live holder without taking the lock.
func Inspect(layout gabitotl.Layout) (Holder, bool) {
	h, err := readHolder(filepath.Join(layout.Local(), LockFile))
	if err != nil || !pidAlive(h.PID) {
		return Holder{}, false
	}
	return h, true
}

// Release removes the lock when this process owns it.
func (l *Lock) Release() error {
	cur, err := readHolder(l.path)
	if err != nil || cur.PID != l.holder.PID {
		return nil
	}
	return os.Remove(l.path)
}
