package compact_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/compact"
	"github.com/winnstorm/gabitotl/internal/memory/store"
	"github.com/winnstorm/gabitotl/internal/memory/store/memstore"
)

var now = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func seed(t *testing.T) (*memstore.Store, *time.Time) {
	t.Helper()
	clock := now.AddDate(0, -3, 0)
	s := memstore.New()
	s.Now = func() time.Time { return clock }
	save := func(typ memory.EntryType, title, content, topic string) {
		t.Helper()
		if _, err := s.Save(context.Background(), memory.Entry{Type: typ, Title: title, Content: content, Project: "p", Actor: "u", TopicKey: topic}); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(time.Hour)
	}
	save(memory.TypeProgress, "step one", "did a", "")
	save(memory.TypeProgress, "step two", "did b", "")
	save(memory.TypeProgress, "step two", "did b", "") // duplicate
	save(memory.TypeDecision, "use X", "because", "")
	save(memory.TypeBugfix, "fix Y", "cause", "")
	save(memory.TypeProgress, "keyed", "revision", "session/summary")
	clock = now.Add(-time.Hour)
	save(memory.TypeProgress, "recent", "fresh", "")
	return s, &clock
}

func TestParseAge(t *testing.T) {
	for in, want := range map[string]time.Duration{"30d": 30 * 24 * time.Hour, "2w": 14 * 24 * time.Hour} {
		if got, err := compact.ParseAge(in); err != nil || got != want {
			t.Errorf("%s: %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "d", "0d", "3x", "-1d", "1.5w"} {
		if _, err := compact.ParseAge(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestPlanExcludesKnowledgeAndRecent(t *testing.T) {
	s, _ := seed(t)
	recs, err := compact.List(context.Background(), s, "p")
	if err != nil {
		t.Fatal(err)
	}
	p := compact.Build(recs, now, compact.DefaultOlderThan)
	for _, g := range p.Groups {
		for _, r := range g.Records {
			if r.Type != memory.TypeProgress || r.TopicKey != "" || r.Title == "recent" {
				t.Errorf("folded %+v", r.Entry)
			}
		}
	}
	// duplicate drops one "step two"; digest folds the remaining two old progress entries.
	if len(p.Groups) != 2 || p.Groups[0].Kind != "duplicate" || len(p.Groups[1].Records) != 2 {
		t.Fatalf("groups = %+v", p.Groups)
	}
	if d := p.Groups[1].Digest; d == nil || !strings.HasPrefix(d.Title, "Progress digest 2026-07") || !strings.Contains(d.Content, "- step one") {
		t.Fatalf("digest = %+v", d)
	}
	if n, b := p.Folded(); n != 3 || b == 0 {
		t.Errorf("folded %d %d", n, b)
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	s, _ := seed(t)
	before, _ := compact.List(context.Background(), s, "p")
	compact.Build(before, now, compact.DefaultOlderThan)
	after, _ := compact.List(context.Background(), s, "p")
	if len(before) != len(after) {
		t.Fatal("Build mutated the store")
	}
}

func TestApplyDigestsSoftDeleteAndBackup(t *testing.T) {
	s, _ := seed(t)
	ctx := context.Background()
	recs, _ := compact.List(ctx, s, "p")
	p := compact.Build(recs, now, compact.DefaultOlderThan)
	path, err := compact.Backup(t.TempDir(), now, p)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var bak struct{ Entries []store.Record }
	if json.Unmarshal(raw, &bak) != nil || len(bak.Entries) != 3 {
		t.Fatalf("backup = %s", raw)
	}
	dg, del, err := compact.Apply(ctx, s, p, "u")
	if err != nil || dg != 1 || del != 3 {
		t.Fatalf("apply %d %d %v", dg, del, err)
	}
	left, _ := compact.List(ctx, s, "p")
	titles := map[string]int{}
	for _, r := range left {
		titles[r.Title]++
	}
	if titles["Progress digest 2026-07"] != 1 || titles["step one"] != 0 || titles["step two"] != 0 || titles["use X"] != 1 || titles["fix Y"] != 1 || titles["keyed"] != 1 || titles["recent"] != 1 {
		t.Fatalf("left = %v", titles)
	}
	// Idempotent: the digest is never folded again.
	if p2 := compact.Build(left, now, compact.DefaultOlderThan); len(p2.Groups) != 0 {
		t.Fatalf("second plan = %+v", p2.Groups)
	}
}

func TestListPagesPastTheStoreLimit(t *testing.T) {
	s := memstore.New()
	for i := 0; i < 120; i++ {
		if _, err := s.Save(context.Background(), memory.Entry{Type: memory.TypeProgress, Title: "t" + string(rune('a'+i%26)) + strings.Repeat("x", i), Content: "c", Project: "p", Actor: "u"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := compact.List(context.Background(), s, "p")
	if err != nil || len(got) != 120 {
		t.Fatalf("got %d %v", len(got), err)
	}
}
