package tokens

import (
	"strings"
	"testing"
)

func TestEstimate(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0}, {"a", 1}, {strings.Repeat("a", 36), 10}, {strings.Repeat("a", 37), 11},
		{"ñ", 1},                        // one rune, not two bytes
		{strings.Repeat("ñ", 36), 10},   // runes are counted, not bytes
		{strings.Repeat("日本語", 12), 10}, // 36 runes
	}
	for _, c := range cases {
		if got := Estimate(c.in); got != c.want {
			t.Errorf("Estimate(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestEstimateLegacy(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{{"", 0}, {"a", 1}, {"abcd", 1}, {"abcde", 2}, {strings.Repeat("ñ", 8), 2}}
	for _, c := range cases {
		if got := EstimateLegacy(c.in); got != c.want {
			t.Errorf("EstimateLegacy(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestEstimateLeansHigherThanLegacy(t *testing.T) {
	s := strings.Repeat("hola mundo ", 50)
	if Estimate(s) <= EstimateLegacy(s) {
		t.Fatalf("Estimate %d should exceed legacy %d", Estimate(s), EstimateLegacy(s))
	}
}

func TestFromBytes(t *testing.T) {
	if FromBytes(-5) != 0 || FromBytes(0) != 0 || FromBytes(36) != 10 {
		t.Fatal("FromBytes edge cases")
	}
}
