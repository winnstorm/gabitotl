package gui

import (
	"context"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/projects"
)

func TestProjectsNilPortIsEmpty(t *testing.T) {
	got, err := New(Deps{}).Projects(context.Background())
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %#v %v", got, err)
	}
}

func TestProjectsDTOAndRemembering(t *testing.T) {
	var added, forgot string
	dir := t.TempDir()
	s := New(Deps{
		Projects: func(context.Context) ([]projects.Project, error) {
			return []projects.Project{
				{Path: "/w/a", Name: "a", Sources: []string{"kiro"}, IsGit: true, LastSeen: time.Unix(0, 0).UTC().Add(time.Hour)},
				{Path: "/w/b", Name: "b"},
			}, nil
		},
		RememberProject: func(p string) error { added = p; return nil },
		ForgetProject:   func(p string) error { forgot = p; return nil },
	})
	got, _ := s.Projects(context.Background())
	if len(got) != 2 || got[0].LastSeen != "1970-01-01T01:00:00Z" || !got[0].IsGit || got[1].LastSeen != "" || got[1].Sources == nil {
		t.Fatalf("dto %+v", got)
	}
	if err := s.AddProject(dir); err != nil || added != dir {
		t.Fatalf("add %q %v", added, err)
	}
	if err := s.AddProject(dir + "/missing"); err == nil {
		t.Fatal("a missing folder must be refused")
	}
	if err := s.ForgetProject("/w/a"); err != nil || forgot != "/w/a" {
		t.Fatalf("forget %q %v", forgot, err)
	}
	if New(Deps{}).AddProject(dir) == nil {
		t.Fatal("no port must be an error")
	}
}
