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

func TestExpandPercent(t *testing.T) {
	env := map[string]string{"LOCALAPPDATA": `C:\L`, "X": "x"}
	look := func(n string) (string, bool) { v, ok := env[n]; return v, ok }
	cases := map[string]string{
		`%LOCALAPPDATA%\bin`: `C:\L\bin`,
		`%X%%X%`:             `xx`,
		`%NOPE%\bin`:         `%NOPE%\bin`,
		`100%`:               `100%`,
		`%%`:                 `%%`,
		`a%X`:                `a%X`,
		`plain`:              `plain`,
	}
	for in, want := range cases {
		if got := ExpandPercent(in, look); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestHasPathEntryMatchesExpandedForm(t *testing.T) {
	old := lookupEnv
	defer func() { lookupEnv = old }()
	lookupEnv = func(n string) (string, bool) {
		if n == "LOCALAPPDATA" {
			return `C:\Users\Ana\AppData\Local`, true
		}
		return "", false
	}
	path := `C:\A;%LOCALAPPDATA%\Programs\GabitoTL`
	dir := `c:\users\ana\appdata\local\programs\gabitotl`
	if !HasPathEntry(path, dir) {
		t.Fatal("expanded entry not recognised")
	}
	if got, changed := AddPathEntry(path, dir); changed || got != path {
		t.Fatalf("add duplicated an expanded entry: %q", got)
	}
	if got, changed := RemovePathEntry(path, dir); changed || got != path {
		t.Fatalf("remove must keep exact-entry semantics: %q", got)
	}
}
