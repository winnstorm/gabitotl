package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanJSONAndText(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := runIn(t, "", false, t.TempDir(), "scan", "--root", root, "--json")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(r.out), &m); err != nil || m["name"] == "" {
		t.Fatalf("bad json: %v\n%s", err, r.out)
	}
	r = runIn(t, "", false, t.TempDir(), "scan", "--root", root)
	if r.code != 0 || !strings.Contains(r.out, "**Stack**") {
		t.Fatalf("%+v", r)
	}
	if r := runIn(t, "", false, t.TempDir(), "scan", "--root", filepath.Join(root, "nope")); r.code != ExitError {
		t.Fatalf("missing root: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, ".gabitotl")); err == nil {
		t.Fatal("scan must not create .gabitotl")
	}
}
