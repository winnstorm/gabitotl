package vcshosts

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBaseURL(t *testing.T) {
	good := map[string][2]string{
		"https://bitbucket.personal.corp:8443": {"https://bitbucket.personal.corp:8443", "bitbucket.personal.corp"},
		"https://Git.Corp/bitbucket/":          {"https://Git.Corp/bitbucket", "git.corp"},
		"http://127.0.0.1:7990":                {"http://127.0.0.1:7990", "127.0.0.1"},
		"http://localhost:7990/bb":             {"http://localhost:7990/bb", "localhost"},
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
	if err := f.Put(Entry{Platform: BitbucketServer, URL: "https://bb.corp:8443/bitbucket/"}, PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := f.Put(Entry{Platform: GitHub, URL: "https://ghe.corp"}, PutOptions{}); err != nil {
		t.Fatal(err)
	}
	e, ok, err := f.Get("BB.corp:7999")
	if err != nil || !ok || e.URL != "https://bb.corp:8443/bitbucket" || e.Platform != BitbucketServer {
		t.Fatalf("get: %+v %v %v", e, ok, err)
	}
	if err := f.Put(Entry{Platform: GitLab, URL: "https://bb.corp"}, PutOptions{}); !errors.Is(err, ErrHostConflict) {
		t.Fatalf("a different server under the same host must conflict: %v", err)
	}
	if err := f.Put(Entry{Platform: GitLab, URL: "https://bb.corp"}, PutOptions{Replace: true}); err != nil {
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
		if err := f.Put(e, PutOptions{}); err == nil {
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
	_ = f.Put(Entry{Platform: BitbucketServer, URL: "https://bb.corp:8443"}, PutOptions{})
	if e, ok := Lookup(f, "bb.corp"); !ok || e.Platform != BitbucketServer {
		t.Fatal("lookup")
	}
	if _, ok := Lookup(nil, "bb.corp"); ok {
		t.Fatal("nil registry")
	}
}

func TestPlainHTTPOnlyForLoopback(t *testing.T) {
	for _, in := range []string{"http://10.0.0.5:7990", "http://bb.corp"} {
		if _, _, err := ParseBaseURL(in); !errors.Is(err, ErrInsecureHTTP) {
			t.Errorf("%q: want ErrInsecureHTTP, got %v", in, err)
		}
	}
	if base, host, err := ParseBaseURLInsecure("http://10.0.0.5:7990", true); err != nil || base != "http://10.0.0.5:7990" || host != "10.0.0.5" {
		t.Fatalf("override: %q %q %v", base, host, err)
	}
	f := NewFile(t.TempDir())
	e := Entry{Platform: BitbucketServer, URL: "http://10.0.0.5:7990"}
	if err := f.Put(e, PutOptions{}); !errors.Is(err, ErrInsecureHTTP) {
		t.Fatalf("Put must refuse plain http: %v", err)
	}
	if err := f.Put(e, PutOptions{AllowInsecureHTTP: true}); err != nil {
		t.Fatal(err)
	}
	if es, err := f.List(); err != nil || len(es) != 1 { // an entry saved with the override still reads back
		t.Fatalf("%v %v", es, err)
	}
}

func TestHostConflictRules(t *testing.T) {
	f := NewFile(t.TempDir())
	base := Entry{Platform: BitbucketServer, URL: "https://bb.corp:8443/bitbucket", SSHHosts: []string{"SSH.bb.corp"}}
	if err := f.Put(base, PutOptions{}); err != nil {
		t.Fatal(err)
	}
	// the same server again (even with other aliases) is an update, not a conflict
	same := Entry{Platform: BitbucketServer, URL: "https://bb.corp:8443/bitbucket/", SSHHosts: []string{"git.bb.corp"}}
	if err := f.Put(same, PutOptions{}); err != nil {
		t.Fatalf("same server: %v", err)
	}
	for name, e := range map[string]Entry{
		"other port":     {Platform: BitbucketServer, URL: "https://bb.corp:9443/bitbucket"},
		"other scheme":   {Platform: BitbucketServer, URL: "http://bb.corp:8443/bitbucket"},
		"other context":  {Platform: BitbucketServer, URL: "https://bb.corp:8443/other"},
		"other platform": {Platform: GitLab, URL: "https://bb.corp:8443/bitbucket"},
		"alias is host":  {Platform: GitLab, URL: "https://other.corp", SSHHosts: []string{"bb.corp"}},
		"alias is alias": {Platform: GitLab, URL: "https://other.corp", SSHHosts: []string{"git.bb.corp"}},
		"host is alias":  {Platform: GitLab, URL: "https://git.bb.corp"},
	} {
		if err := f.Put(e, PutOptions{AllowInsecureHTTP: true}); !errors.Is(err, ErrHostConflict) {
			t.Errorf("%s: want ErrHostConflict, got %v", name, err)
		}
	}
	if es, _ := f.List(); len(es) != 1 || es[0].URL != "https://bb.corp:8443/bitbucket" {
		t.Fatalf("a refused Put must change nothing: %+v", es)
	}
}

func TestSSHAliasLookup(t *testing.T) {
	f := &Fake{}
	if err := f.Put(Entry{Platform: BitbucketServer, URL: "https://bb.corp", SSHHosts: []string{"ssh.bb.corp", "ssh.bb.corp", "bb.corp"}}, PutOptions{}); err != nil {
		t.Fatal(err)
	}
	e, ok := Lookup(f, "SSH.BB.corp")
	if !ok || e.Host != "bb.corp" || len(e.SSHHosts) != 1 {
		t.Fatalf("alias lookup: %+v %v", e, ok)
	}
	for _, bad := range []string{"a/b", "u@h", "bad_host"} {
		if err := f.Put(Entry{Platform: GitLab, URL: "https://gl.corp", SSHHosts: []string{bad}}, PutOptions{}); err == nil {
			t.Errorf("alias %q should be refused", bad)
		}
	}
}

func TestLookupErrSurfacesAReadError(t *testing.T) {
	home := t.TempDir()
	p := FilePath(home)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("{nope"), 0o644)
	_, ok, err := LookupErr(NewFile(home), "bb.corp")
	if ok || err == nil || !strings.Contains(err.Error(), p) {
		t.Fatalf("want an error naming %s, got ok=%v err=%v", p, ok, err)
	}
	if _, ok := Lookup(NewFile(home), "bb.corp"); ok {
		t.Fatal("Lookup (inference) stays false on a read error")
	}
}

func TestPutKeepsAnotherProcessesEntry(t *testing.T) {
	home := t.TempDir()
	a, b := NewFile(home), NewFile(home) // two handles = two processes
	if err := a.Put(Entry{Platform: GitHub, URL: "https://ghe.corp"}, PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := b.Put(Entry{Platform: GitLab, URL: "https://gl.corp"}, PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if es, _ := a.List(); len(es) != 2 {
		t.Fatalf("the second writer lost the first's entry: %+v", es)
	}
}
