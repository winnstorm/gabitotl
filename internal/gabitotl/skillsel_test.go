package gabitotl

import (
	"slices"
	"testing"
)

func TestOptionalSkillsRoundTrip(t *testing.T) {
	home := t.TempDir()
	if got := LoadOptionalSkills(home); len(got) != 0 {
		t.Fatalf("default must be none, got %v", got)
	}
	if _, err := SaveOptionalSkills(home, []string{"b", "a", "a"}); err != nil {
		t.Fatal(err)
	}
	if got := LoadOptionalSkills(home); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("got %v", got)
	}
	if _, err := SaveOptionalSkills(home, nil); err != nil {
		t.Fatal(err)
	}
	if got := LoadOptionalSkills(home); len(got) != 0 {
		t.Fatalf("cleared selection read %v", got)
	}
}
