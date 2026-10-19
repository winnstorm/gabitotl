package digest

import (
	"strings"
	"testing"
)

func has(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func TestDigest(t *testing.T) {
	goTest := "=== RUN   TestA\n--- PASS: TestA (0.00s)\n=== RUN   TestB\n    b_test.go:12: want 1, got 2\n--- FAIL: TestB (0.00s)\nFAIL\nFAIL\tgithub.com/x/y\t0.004s\n"
	pytest := "collected 3 items\n\ntests/test_a.py ..F\n\n=== FAILURES ===\n___ test_b ___\n    def test_b():\n>       assert f() == 2\nE       AssertionError: assert 1 == 2\n\ntests/test_a.py:7: AssertionError\n=== 1 failed, 2 passed in 0.12s ===\n"
	jest := "npm ERR! code ELIFECYCLE\nnpm ERR! errno 1\n● Suite › case\n  expect(received).toBe(expected)\n"
	tsc := "src/a.ts(10,5): error TS2322: Type 'string' is not assignable to type 'number'.\nsrc/b.ts:3:1 - error TS2304: Cannot find name 'x'.\n"
	eslint := "/p/src/a.js\n  4:7  error  'x' is defined but never used  no-unused-vars\n\n✖ 1 problem (1 error, 0 warnings)\n"
	gobuild := "# pkg\n./main.go:9:2: undefined: foo\n"

	cases := []struct {
		name string
		out  string
		want []string
	}{
		{"go test", goTest, []string{"=== RUN   TestB", "--- FAIL: TestB"}},
		{"pytest", pytest, []string{"AssertionError: assert 1 == 2"}},
		{"jest", jest, []string{"npm ERR! code ELIFECYCLE"}},
		{"tsc", tsc, []string{"TS2322", "src/b.ts:3:1"}},
		{"eslint", eslint, []string{"'x' is defined but never used"}},
		{"go build", gobuild, []string{"undefined: foo"}},
	}
	for _, c := range cases {
		r := Digest(c.out, Options{})
		for _, w := range c.want {
			if !has(r.Highlights, w) {
				t.Errorf("%s: highlights lack %q: %v", c.name, w, r.Highlights)
			}
		}
	}
}

func TestQuietSuccess(t *testing.T) {
	out := strings.Repeat("ok  \tpkg\t0.1s\n", 1) + "a\nb\nc\nd\ne\nf\nTests: 5 passed, 0 failed\n"
	r := Digest(out, Options{OK: true})
	if len(r.Highlights) != 0 {
		t.Fatalf("0 failed must not highlight: %v", r.Highlights)
	}
	if len(r.Tail) != QuietTail {
		t.Fatalf("tail = %d, want %d: %v", len(r.Tail), QuietTail, r.Tail)
	}
}

func TestCollapseAndDedup(t *testing.T) {
	out := strings.Repeat("warn: retry\n", 3) + strings.Repeat("ERROR boom\n", 40) + "end\n"
	r := Digest(out, Options{Tail: 1})
	if !has(r.Highlights, "ERROR boom") || has(r.Highlights, "(×40)") {
		t.Fatalf("duplicates must be deduplicated: %v", r.Highlights)
	}
	if got := collapse([]string{"a", "a", "a", "b"}); got[0] != "a (×3)" || got[1] != "b" {
		t.Fatalf("collapse = %v", got)
	}
}

func TestCapAndTail(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 100; i++ {
		b.WriteString("error: problem number " + strings.Repeat("x", i) + "\n")
	}
	r := Digest(b.String(), Options{MaxLines: 10, Tail: 3})
	if len(r.Highlights) > 11 || !strings.Contains(r.Highlights[len(r.Highlights)-1], "more failure lines") {
		t.Fatalf("cap not applied: %d %v", len(r.Highlights), r.Highlights[len(r.Highlights)-1])
	}
	if r.Lines != 100 {
		t.Fatalf("lines = %d", r.Lines)
	}
}

func TestEmpty(t *testing.T) {
	r := Digest("", Options{OK: true})
	if r.Lines != 0 || len(r.Tail) != 0 {
		t.Fatalf("%+v", r)
	}
}
