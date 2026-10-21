package vcshosts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseBaseURL(t *testing.T) {
	good := map[string][2]string{
		"https://bitbucket.personal.corp:8443": {"https://bitbucket.personal.corp:8443", "bitbucket.personal.corp"},
		"https://Git.Corp/bitbucket/":          {"https://Git.Corp/bitbucket", "git.corp"},
		"http://10.0.0.5:7990":                 {"http://10.0.0.5:7990", "10.0.0.5"},
	}
	for in, want := range good {
		base, host, err := ParseBaseURL(in)
		if err != nil || base != want[0] || host != want[1] {
			t.Errorf("%q: got %q %q %v", in, base, host, err)
		}
	}
	for _, bad := range []string{"", "ftp://x.corp", "git.corp", "https://u:p@git.corp", "https://git.corp?x=1", "https://git.corp/#a", "https:///x", "https://git.corp:ab", "https://bad_host.corp"} {
		if _, _, err := ParseBaseURL(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestFileRoundTrip(t *testing.T) {
	home := t.TempDir()
	f := NewFile(home)
	if es, err := f.List(); err != nil || len(es) != 0 {
		t.Fatalf("empty: %v %v", es, err)
	}
	if err := f.Put(Entry{Platform: BitbucketServer, URL: "https://bb.corp:8443/bitbucket/"}); err != nil {
		t.Fatal(err)
	}
	if err := f.Put(Entry{Platform: GitHub, URL: "https://ghe.corp"}); err != nil {
		t.Fatal(err)
	}
	e, ok, err := f.Get("BB.corp:7999")
	if err != nil || !ok || e.URL != "https://bb.corp:8443/bitbucket" || e.Platform != BitbucketServer {
		t.Fatalf("get: %+v %v %v", e, ok, err)
	}
	if err := f.Put(Entry{Platform: GitLab, URL: "https://bb.corp"}); err != nil {
		t.Fatal(err)
	}
	es, _ := f.List()
	if len(es) != 2 || es[0].Host != "bb.corp" || es[0].Platform != GitLab {
		t.Fatalf("replace/sort: %+v", es)
	}
	if err := f.Delete("ghe.corp"); err != nil {
		t.Fatal(err)
	}
	if err := f.Delete("missing.corp"); err != nil {
		t.Fatal(err)
	}
	if es, _ := f.List(); len(es) != 1 {
		t.Fatalf("delete: %+v", es)
	}
	if m, _ := filepath.Glob(filepath.Join(home, ".gabitotl", ".vcs-hosts-*")); len(m) != 0 {
		t.Fatalf("temp files left: %v", m)
	}
}

func TestPutValidates(t *testing.T) {
	f := NewFile(t.TempDir())
	for _, e := range []Entry{
		{Platform: "svn", URL: "https://a.corp"},
		{Platform: GitHub, URL: "https://u:p@a.corp"},
		{Platform: GitHub, URL: "https://a.corp?x=1"},
		{Platform: GitHub, URL: "ftp://a.corp"},
		{Platform: GitHub, Host: "other.corp", URL: "https://a.corp"},
	} {
		if err := f.Put(e); err == nil {
			t.Errorf("%+v should be refused", e)
		}
	}
	if es, _ := f.List(); len(es) != 0 {
		t.Fatal("nothing should be stored")
	}
}

func TestNoHomeAndCorruptFile(t *testing.T) {
	if _, err := NewFile("").List(); err != ErrNoHome {
		t.Fatalf("got %v", err)
	}
	if _, err := NewFile("relative/home").List(); err != ErrNoHome {
		t.Fatalf("got %v", err)
	}
	home := t.TempDir()
	p := FilePath(home)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("{nope"), 0o644)
	if _, err := NewFile(home).List(); err == nil {
		t.Fatal("corrupt file must error")
	}
	os.WriteFile(p, []byte(`{"hosts":[{"host":"x.corp","platform":"svn","url":"https://x.corp"},{"host":"y.corp","platform":"gitlab","url":"https://y.corp"}]}`), 0o644)
	if es, _ := NewFile(home).List(); len(es) != 1 || es[0].Host != "y.corp" {
		t.Fatalf("bad entries must be skipped: %+v", es)
	}
}

func TestFakeAndLookup(t *testing.T) {
	f := &Fake{}
	_ = f.Put(Entry{Platform: BitbucketServer, URL: "https://bb.corp:8443"})
	if e, ok := Lookup(f, "bb.corp"); !ok || e.Platform != BitbucketServer {
		t.Fatal("lookup")
	}
	if _, ok := Lookup(nil, "bb.corp"); ok {
		t.Fatal("nil registry")
	}
}
