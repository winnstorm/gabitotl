package desktop

import "testing"

func TestAddPathEntry(t *testing.T) {
	cases := []struct {
		name, path, dir, want string
		changed               bool
	}{
		{"empty path", "", `C:\G`, `C:\G`, true},
		{"append", `C:\A;C:\B`, `C:\G`, `C:\A;C:\B;C:\G`, true},
		{"trailing semicolon", `C:\A;`, `C:\G`, `C:\A;C:\G`, true},
		{"drops empty entries on change", `C:\A;;C:\B;`, `C:\G`, `C:\A;C:\B;C:\G`, true},
		{"present", `C:\A;C:\G`, `C:\G`, `C:\A;C:\G`, false},
		{"present other case", `C:\A;c:\g`, `C:\G`, `C:\A;c:\g`, false},
		{"present trailing slash", `C:\A;C:\G\`, `C:\G`, `C:\A;C:\G\`, false},
		{"present quoted", `"C:\G"`, `C:\G`, `"C:\G"`, false},
		{"blank dir", `C:\A`, `  `, `C:\A`, false},
		{"keeps expand vars", `%USERPROFILE%\bin`, `C:\G`, `%USERPROFILE%\bin;C:\G`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := AddPathEntry(c.path, c.dir)
			if got != c.want || changed != c.changed {
				t.Fatalf("got (%q,%v), want (%q,%v)", got, changed, c.want, c.changed)
			}
		})
	}
}

func TestRemovePathEntry(t *testing.T) {
	cases := []struct {
		name, path, dir, want string
		changed               bool
	}{
		{"only entry", `C:\G`, `C:\G`, ``, true},
		{"middle", `C:\A;C:\G;C:\B`, `C:\G`, `C:\A;C:\B`, true},
		{"case-insensitive", `C:\A;c:\g\`, `C:\G`, `C:\A`, true},
		{"duplicates", `C:\G;C:\A;C:\g`, `C:\G`, `C:\A`, true},
		{"trailing semicolon and empties", `C:\A;;C:\G;`, `C:\G`, `C:\A`, true},
		{"absent", `C:\A;;`, `C:\G`, `C:\A;;`, false},
		{"empty", ``, `C:\G`, ``, false},
		{"prefix is not a match", `C:\GabitoTL2`, `C:\GabitoTL`, `C:\GabitoTL2`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := RemovePathEntry(c.path, c.dir)
			if got != c.want || changed != c.changed {
				t.Fatalf("got (%q,%v), want (%q,%v)", got, changed, c.want, c.changed)
			}
		})
	}
}

func TestHasPathEntry(t *testing.T) {
	if HasPathEntry(`C:\A;C:\B`, ``) {
		t.Fatal("empty dir must never match")
	}
	if !HasPathEntry(` C:\g ;x`, `c:\G\`) {
		t.Fatal("whitespace, case and trailing slash must be ignored")
	}
}
