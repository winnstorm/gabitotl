package budget

import (
	"os"
	"testing"
)

// TestMain isolates the home directory: a saved persona language (lang: en) in
// the real home must not change the messages the tests assert.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "gabitotl-home-")
	if err == nil {
		os.Setenv("HOME", home)
		os.Setenv("USERPROFILE", home)
	}
	code := m.Run()
	if err == nil {
		os.RemoveAll(home)
	}
	os.Exit(code)
}
