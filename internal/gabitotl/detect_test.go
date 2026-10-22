package gabitotl

import "testing"

func TestIsFilesystemRootCoversDrivesAndShares(t *testing.T) {
	for p, want := range map[string]bool{
		"/": true, "C:": true, `C:\`: true, "c:/": true, `\\srv\share`: true, `\\srv\share\`: true, "//srv/share": true,
		`C:\work`: false, `\\srv\share\dir`: false, "/home/a": false, "": false, "d:/x/y": false,
	} {
		if got := IsFilesystemRoot(p); got != want {
			t.Errorf("IsFilesystemRoot(%q) = %v, want %v", p, got, want)
		}
	}
	if ValidateRoot(`D:\`) == nil || ValidateRoot(`\\srv\share`) == nil {
		t.Error("ValidateRoot must refuse drive and share roots")
	}
}
