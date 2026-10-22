//go:build !windows

package credstore

// New returns the credential store of the user whose home folder is home: a 0600
// file under ~/.gabitotl/secrets/ on this OS.
func New(home string) Store { return NewFile(home) }
