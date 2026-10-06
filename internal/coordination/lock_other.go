//go:build !unix

package coordination

// processAlive is conservative where signal 0 is unavailable: a lock is never
// treated as stale automatically, so a leftover lock must be removed by hand.
func processAlive(pid int) bool { return true }
