package budget

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// SavingsFile is the per-person, append-only log of bytes kept out of the
// agent's context, in .gabitotl/local/users/<id>/. One line per event:
// "<unix> <kind> <full_bytes> <printed_bytes>". It is never committed.
const SavingsFile = "savings.log"

// RecordSavings appends one savings event. It is best-effort: callers ignore
// the error so statistics never fail a command.
func RecordSavings(layout gabitotl.Layout, user, kind string, full, printed int) error {
	dir, err := layout.LocalUserDir(user)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, SavingsFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%d %s %d %d\n", time.Now().Unix(), kind, full, printed)
	return err
}
