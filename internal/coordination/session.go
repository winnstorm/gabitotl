package coordination

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"gopkg.in/yaml.v3"
)

// SessionsDir is where live sessions register, inside .gabitotl/local/ (never
// committed). Several people share it, so it is group-writable (subject to the
// umask) and each person writes only their own files.
const SessionsDir = "sessions"

// SessionTTL bounds how long a session stays live without a refresh, even when
// its anchor PID still exists (PIDs get reused, and a shell can outlive a
// forgotten session). Long-lived anchors refresh every HeartbeatInterval.
const SessionTTL = 12 * time.Hour

// HeartbeatInterval is how often a long-lived session refreshes last_seen.
const HeartbeatInterval = 10 * time.Minute

// Session is one live use of a working directory by one person. The file name
// is <user>-<pid>.yaml, so a person can have several (two terminals, two agents).
//
// PID is the liveness anchor, which is not always the process that wrote the
// file. GabitoTL has two kinds of entry point:
//   - long-lived ones that stay up for the whole agent session (the stdio memory
//     servers an agent runtime spawns): their own PID is the anchor;
//   - short-lived ones (`work start`, Git hooks): the process exits within
//     milliseconds, so its own PID says nothing. The anchor is the parent (the
//     user's shell or the agent), see AnchorPID. When even the parent is a
//     throwaway shell, last_seen and SessionTTL retire the entry.
type Session struct {
	User     string    `yaml:"user" json:"user"`
	PID      int       `yaml:"pid" json:"pid"`
	Started  time.Time `yaml:"started" json:"started"`
	LastSeen time.Time `yaml:"last_seen" json:"last_seen"`
	Task     string    `yaml:"task,omitempty" json:"task,omitempty"`
	Branch   string    `yaml:"branch,omitempty" json:"branch,omitempty"`
	Areas    []string  `yaml:"areas,omitempty" json:"areas,omitempty"`
	Runtime  string    `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	// Own is set by Sessions when the session belongs to the asking user.
	Own bool `yaml:"-" json:"own"`
}

// pidAlive is replaced in tests. See process_unix.go for the real check.
var pidAlive = processAlive

// AnchorPID returns the PID that stands for this invocation in the registry:
// the process itself when it lives as long as the agent session, otherwise its
// parent. A parent that is gone or is init (PID 1) is no anchor, so the
// process falls back to its own PID.
func AnchorPID(longLived bool) int {
	if longLived {
		return os.Getpid()
	}
	if pp := os.Getppid(); pp > 1 {
		return pp
	}
	return os.Getpid()
}

func sessionsPath(layout gabitotl.Layout) string {
	return filepath.Join(layout.Local(), SessionsDir)
}

func sessionFile(layout gabitotl.Layout, user string, pid int) string {
	return filepath.Join(sessionsPath(layout), fmt.Sprintf("%s-%d.yaml", user, pid))
}

// Register records s as live at now. It is idempotent per user and PID: the
// start time of an existing entry is kept, last_seen is refreshed and the
// optional fields are replaced only when s sets them. A failure (read-only
// checkout, a directory another OS user owns) is returned for the caller to
// show as a warning; presence is advisory and must never stop work.
func Register(layout gabitotl.Layout, s Session, now time.Time) error {
	if !gabitotl.ValidID(s.User) {
		return gabitotl.ErrInvalidID
	}
	if s.PID <= 0 {
		return errors.New("coordination: session needs a PID")
	}
	path := sessionFile(layout, s.User, s.PID)
	s.Started = now.UTC()
	if cur, err := readSession(path); err == nil {
		s.Started = cur.Started
		if s.Task == "" {
			s.Task = cur.Task
		}
		if s.Branch == "" {
			s.Branch = cur.Branch
		}
		if len(s.Areas) == 0 {
			s.Areas = cur.Areas
		}
		if s.Runtime == "" {
			s.Runtime = cur.Runtime
		}
	}
	s.LastSeen = now.UTC()
	return writeSession(layout, path, s)
}

// Touch refreshes last_seen of an existing session. It reports whether the
// session existed; a missing one is not created.
func Touch(layout gabitotl.Layout, user string, pid int, now time.Time) (bool, error) {
	if !gabitotl.ValidID(user) {
		return false, gabitotl.ErrInvalidID
	}
	path := sessionFile(layout, user, pid)
	s, err := readSession(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	s.LastSeen = now.UTC()
	return true, writeSession(layout, path, s)
}

// Unregister removes the session of user anchored at pid. A missing file is not
// an error.
func Unregister(layout gabitotl.Layout, user string, pid int) error {
	if !gabitotl.ValidID(user) {
		return gabitotl.ErrInvalidID
	}
	if err := os.Remove(sessionFile(layout, user, pid)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Heartbeat refreshes the session every interval until ctx ends. Long-lived
// entry points run it so a session older than SessionTTL does not look dead.
func Heartbeat(ctx context.Context, layout gabitotl.Layout, user string, pid int, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			_, _ = Touch(layout, user, pid, now)
		}
	}
}

// Sessions returns the live sessions of the working directory, oldest first.
// A session is live when its anchor PID exists and last_seen is within
// SessionTTL of now. Dead ones are pruned lazily here (best effort: a file
// another OS user owns that cannot be removed is just skipped). Sessions of
// self are flagged Own. A missing directory is no sessions, not an error.
func Sessions(layout gabitotl.Layout, self string, now time.Time) ([]Session, error) {
	entries, err := os.ReadDir(sessionsPath(layout))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var live []Session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(sessionsPath(layout), e.Name())
		s, err := readSession(path)
		if err != nil {
			continue // unreadable or from a newer version: not ours to delete
		}
		if now.Sub(s.LastSeen) > SessionTTL || !pidAlive(s.PID) {
			_ = os.Remove(path)
			continue
		}
		s.Own = s.User == self
		live = append(live, s)
	}
	sort.SliceStable(live, func(i, j int) bool { return live[i].Started.Before(live[j].Started) })
	return live, nil
}

// Others filters out the sessions of the asking user.
func Others(sessions []Session) []Session {
	var out []Session
	for _, s := range sessions {
		if !s.Own {
			out = append(out, s)
		}
	}
	return out
}

// Notice renders the advisory line for one session, for example
// "ana is also active in this directory since 12 min ago (task auth-refactor)".
func Notice(s Session, now time.Time) string {
	line := fmt.Sprintf("%s is also active in this directory since %s", s.User, ago(now.Sub(s.Started)))
	var extra []string
	if s.Task != "" {
		extra = append(extra, "task "+s.Task)
	}
	if s.Branch != "" {
		extra = append(extra, "branch "+s.Branch)
	}
	if len(extra) > 0 {
		line += " (" + strings.Join(extra, ", ") + ")"
	}
	return line
}

// Notices renders one line per person with a live session in others, using that
// person's longest-running session. Nothing is returned when nobody else is here.
func Notices(sessions []Session, now time.Time) []string {
	var lines []string
	seen := map[string]bool{}
	for _, s := range sessions { // oldest first
		if s.Own || seen[s.User] {
			continue
		}
		seen[s.User] = true
		lines = append(lines, Notice(s, now))
	}
	return lines
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "moments ago"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d/time.Hour))
	default:
		return fmt.Sprintf("%d days ago", int(d/(24*time.Hour)))
	}
}

func readSession(path string) (Session, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Session{}, err
	}
	var s Session
	if err := yaml.Unmarshal(b, &s); err != nil {
		return Session{}, err
	}
	if s.PID <= 0 || !gabitotl.ValidID(s.User) {
		return Session{}, errors.New("invalid session file")
	}
	return s, nil
}

// writeSession replaces path atomically (temp file in the same directory, then
// rename) so a reader never sees a half-written entry. The directory and file
// are group-writable (subject to the umask) because several OS users share it.
func writeSession(layout gabitotl.Layout, path string, s Session) error {
	if err := os.MkdirAll(sessionsPath(layout), 0o775); err != nil {
		return err
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	// The temp name is unique per user and PID, and 0o664 goes through the umask.
	name := filepath.Join(filepath.Dir(path), ".tmp-"+filepath.Base(path))
	tmp, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o664)
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(name, path)
	}
	if werr != nil {
		_ = os.Remove(name)
	}
	return werr
}
