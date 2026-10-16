//go:build !unix

package coordination

// processAlive is conservative where signal 0 is unavailable: a session is
// never treated as dead by its PID, so only the session TTL retires it.
func processAlive(pid int) bool { return true }
