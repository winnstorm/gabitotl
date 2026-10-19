package vcs

import "testing"

func TestRemoteHostDropsCredentialsAndPort(t *testing.T) {
	for in, want := range map[string]string{
		"https://user:secret@GitHub.com:8443/acme/shop.git": "github.com",
		"git@gitlab.example.org:team/app.git":               "gitlab.example.org",
		"  https://bitbucket.org/acme/shop  ":               "bitbucket.org",
		"":                                                  "",
	} {
		if got := RemoteHost(in); got != want {
			t.Errorf("RemoteHost(%q) = %q, want %q", in, got, want)
		}
	}
}
