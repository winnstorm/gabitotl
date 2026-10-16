package vcs

import "testing"

func TestIsGenerated(t *testing.T) {
	yes := []string{"go.sum", "sub/package-lock.json", "yarn.lock", "pnpm-lock.yaml", "web/app.min.js", "x/__snapshots__/a.snap",
		"vendor/lib/a.go", "ui/node_modules/x/y.js", "api/api.pb.go", "gen/types_generated.go", "a.generated.ts", "pkg/{old => go.sum}"}
	no := []string{"main.go", "docs/vendor-notes.md", "src/lockfile.go", "README.md"}
	for _, p := range yes {
		if !IsGenerated(p) {
			t.Errorf("%s must be generated", p)
		}
	}
	for _, p := range no {
		if IsGenerated(p) {
			t.Errorf("%s must count", p)
		}
	}
}

func TestCountNumstatSkipsBinaryAndGenerated(t *testing.T) {
	out := "3\t1\tmain.go\n-\t-\timg.png\n900\t0\tgo.sum\n2\t2\tdocs/a.md\n"
	if got := countNumstat(out); got != 8 {
		t.Fatalf("got %d", got)
	}
}

func TestSliceCommitsGreedyAtBoundaries(t *testing.T) {
	cs := func(ls ...int) []shipCommit {
		var out []shipCommit
		for _, l := range ls {
			out = append(out, shipCommit{lines: l})
		}
		return out
	}
	shape := func(s [][]shipCommit) []int {
		var n []int
		for _, x := range s {
			n = append(n, len(x))
		}
		return n
	}
	s, over := sliceCommits(cs(4, 4, 4, 4), 10)
	if got := shape(s); len(got) != 2 || got[0] != 2 || got[1] != 2 || len(over) != 0 {
		t.Fatalf("%v %v", got, over)
	}
	s, over = sliceCommits(cs(3, 25, 3), 10)
	if got := shape(s); len(got) != 3 || len(over) != 1 || over[0].lines != 25 {
		t.Fatalf("%v %v", got, over)
	}
	s, _ = sliceCommits(cs(10, 1), 10)
	if len(s) != 2 {
		t.Fatalf("a slice never exceeds the budget: %v", shape(s))
	}
}
