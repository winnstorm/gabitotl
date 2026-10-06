// Package storetest is the conformance suite every store.Store must pass. It
// runs the same assertions against memstore (always) and pgstore (when an
// isolated PostgreSQL is available), so the in-memory reference and the real
// store cannot drift apart.
package storetest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/store"
)

// Factory returns a fresh, empty store for one subtest. Cleanup is the factory's job.
type Factory func(t *testing.T) store.Store

func entry(project, actor, title, content string) memory.Entry {
	return memory.Entry{Type: memory.TypeDecision, Title: title, Content: content, Project: project, Actor: actor}
}

func mustSave(t *testing.T, s store.Store, e memory.Entry) store.SaveResult {
	t.Helper()
	r, err := s.Save(context.Background(), e)
	if err != nil {
		t.Fatalf("save %q: %v", e.Title, err)
	}
	return r
}

func titles(rs []store.Record) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Title)
	}
	return out
}

// Run executes the whole suite.
func Run(t *testing.T, f Factory) {
	cases := []struct {
		name string
		fn   func(*testing.T, store.Store)
	}{
		{"SaveGetRoundTrip", saveGet},
		{"WriteValidation", writeValidation},
		{"UpsertByTopicKey", upsert},
		{"ConcurrentUpsert", concurrentUpsert},
		{"UpdateAndRevision", update},
		{"SoftDelete", softDelete},
		{"SearchRelevance", searchRelevance},
		{"SearchFiltersAndCaps", searchFilters},
		{"SearchIsInjectionSafe", searchSafe},
		{"ActiveWork", active},
		{"Summaries", summaries},
		{"Tokens", tokens},
		{"DoctorAndStats", doctorStats},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.fn(t, f(t)) })
	}
}

func saveGet(t *testing.T, s store.Store) {
	ctx := context.Background()
	in := memory.Entry{
		Type: memory.TypeBugfix, Title: "Fix refresh", Content: "expired token was reusable", Project: "shop",
		TopicKey: "auth/refresh", Actor: "gabi", Task: "auth-login", Branch: "feature/auth", Commit: "abc123",
		Files: []string{"a.go", "b/c.go"}, Validation: "tests ok", NextStep: "logout", Ojo: "cache", Source: "gabitotl",
	}
	res := mustSave(t, s, in)
	if res.ID == "" || res.Revision != 1 || !res.Created {
		t.Fatalf("result: %+v", res)
	}
	got, err := s.Get(ctx, "shop", res.ID)
	if err != nil {
		t.Fatal(err)
	}
	in.ID = res.ID
	got.Timestamp = time.Time{}
	if fmt.Sprint(got.Entry) != fmt.Sprint(in) {
		t.Fatalf("round trip differs:\n got %+v\nwant %+v", got.Entry, in)
	}
	if got.Revision != 1 || got.CreatedBy != "gabi" || got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() || got.Scope != store.DefaultScope {
		t.Fatalf("bookkeeping: %+v", got)
	}
	if _, err := s.Get(ctx, "other", res.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("other project must not see the entry: %v", err)
	}
	if _, err := s.Get(ctx, "shop", "00000000-0000-4000-8000-000000000000"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
	// An id that is not a UUID must be "not found", never a database error.
	if _, err := s.Get(ctx, "shop", "no-such-id"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("malformed id: %v", err)
	}
	blank := mustSave(t, s, entry("shop", "gabi", "No source", "body"))
	if g, _ := s.Get(ctx, "shop", blank.ID); g.Source != memory.DefaultSource {
		t.Fatalf("default source: %q", g.Source)
	}
}

func writeValidation(t *testing.T, s store.Store) {
	ctx := context.Background()
	bad := []memory.Entry{
		entry("", "gabi", "t", "c"),
		entry("p", "", "t", "c"),
		entry("p", "  ", "t", "c"),
		entry("p", "gabi", "", "c"),
		entry("p", "gabi", "t", ""),
		{Type: "nonsense", Title: "t", Content: "c", Project: "p", Actor: "gabi"},
	}
	for i, e := range bad {
		if _, err := s.Save(ctx, e); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("case %d: want ErrInvalid, got %v", i, err)
		}
	}
	if _, err := s.Save(ctx, entry("p", memory.ActorLegacy, "t", "c")); err != nil {
		t.Fatalf("legacy actor must be accepted for imports: %v", err)
	}
}

func upsert(t *testing.T, s store.Store) {
	ctx := context.Background()
	e := entry("shop", "gabi", "Auth model v1", "sessions")
	e.TopicKey = "arch/auth"
	first := mustSave(t, s, e)
	e.Title, e.Content, e.Actor = "Auth model v2", "jwt", "gonza"
	second := mustSave(t, s, e)
	if second.ID != first.ID || second.Created || second.Revision != 2 {
		t.Fatalf("upsert must revise in place: %+v then %+v", first, second)
	}
	got, _ := s.Get(ctx, "shop", first.ID)
	if got.Title != "Auth model v2" || got.Actor != "gonza" || got.CreatedBy != "gabi" {
		t.Fatalf("author of the revision is the last writer, creator is kept: %+v", got)
	}
	e.Project = "other"
	if third := mustSave(t, s, e); third.ID == first.ID || !third.Created {
		t.Fatalf("same topic_key in another project is a different entry: %+v", third)
	}
	if err := s.Delete(ctx, "shop", first.ID, "gabi"); err != nil {
		t.Fatal(err)
	}
	e.Project = "shop"
	if again := mustSave(t, s, e); again.ID == first.ID || !again.Created || again.Revision != 1 {
		t.Fatalf("a deleted entry must not capture the upsert: %+v", again)
	}
	// Entries without a topic_key never collapse.
	a, b := mustSave(t, s, entry("shop", "gabi", "same", "same")), mustSave(t, s, entry("shop", "gabi", "same", "same"))
	if a.ID == b.ID {
		t.Fatal("entries without topic_key must be distinct")
	}
}

func concurrentUpsert(t *testing.T, s store.Store) {
	ctx := context.Background()
	const n = 16
	var wg sync.WaitGroup
	ids := make([]memory.ID, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := entry("shop", "gabi", "Racing", fmt.Sprintf("writer %d", i))
			e.TopicKey = "race/key"
			r, err := s.Save(ctx, e)
			ids[i], errs[i] = r.ID, err
		}()
	}
	wg.Wait()
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("writer %d: %v", i, errs[i])
		}
		if ids[i] != ids[0] {
			t.Fatalf("concurrent upserts produced two entries: %q vs %q", ids[i], ids[0])
		}
	}
	rs, _ := s.Search(ctx, memory.Query{Text: "racing", Project: "shop"})
	if len(rs) != 1 || rs[0].Revision != n {
		t.Fatalf("want one entry at revision %d, got %d entries (%+v)", n, len(rs), rs)
	}
}

func update(t *testing.T, s store.Store) {
	ctx := context.Background()
	res := mustSave(t, s, entry("shop", "gabi", "Old title", "old body"))
	title, next := "New title", "ship it"
	rec, err := s.Update(ctx, "shop", res.ID, memory.EntryPatch{Title: &title, NextStep: &next, Files: []string{"x.go"}}, "gonza", 1)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Title != title || rec.Content != "old body" || rec.NextStep != next || rec.Revision != 2 || rec.Actor != "gonza" || rec.CreatedBy != "gabi" || len(rec.Files) != 1 {
		t.Fatalf("patch result: %+v", rec)
	}
	if _, err := s.Update(ctx, "shop", res.ID, memory.EntryPatch{Title: &title}, "gabi", 1); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale revision must conflict, got %v", err)
	}
	if rec, err = s.Update(ctx, "shop", res.ID, memory.EntryPatch{Title: &title}, "gabi", 0); err != nil || rec.Revision != 3 {
		t.Fatalf("revision 0 skips the check: %+v %v", rec, err)
	}
	if _, err := s.Update(ctx, "other", res.ID, memory.EntryPatch{Title: &title}, "gabi", 0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("wrong project: %v", err)
	}
	if _, err := s.Update(ctx, "shop", res.ID, memory.EntryPatch{Title: &title}, "", 0); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("actor required on update: %v", err)
	}
	empty := " "
	if _, err := s.Update(ctx, "shop", res.ID, memory.EntryPatch{Title: &empty}, "gabi", 0); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("a patch cannot blank the title: %v", err)
	}
	if _, err := s.Update(ctx, "", "11111111-1111-4111-8111-111111111111", memory.EntryPatch{Title: &title}, "gabi", 0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
	// Concurrent optimistic updates: exactly one wins per revision.
	cur, _ := s.Get(ctx, "shop", res.ID)
	var wg sync.WaitGroup
	var mu sync.Mutex
	won, lost := 0, 0
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := fmt.Sprintf("racer %d", i)
			_, err := s.Update(ctx, "shop", res.ID, memory.EntryPatch{Content: &c}, "gabi", cur.Revision)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				won++
			} else if errors.Is(err, store.ErrConflict) {
				lost++
			} else {
				t.Errorf("unexpected: %v", err)
			}
		}()
	}
	wg.Wait()
	if won != 1 || lost != 7 {
		t.Fatalf("optimistic locking: won=%d lost=%d", won, lost)
	}
}

func softDelete(t *testing.T, s store.Store) {
	ctx := context.Background()
	res := mustSave(t, s, entry("shop", "gabi", "Ephemeral thing", "gone soon"))
	if err := s.Delete(ctx, "other", res.ID, "gabi"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("wrong project: %v", err)
	}
	if err := s.Delete(ctx, "shop", res.ID, ""); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("actor required: %v", err)
	}
	if err := s.Delete(ctx, "shop", res.ID, "gabi"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "shop", res.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
	if rs, _ := s.Search(ctx, memory.Query{Text: "ephemeral", Project: "shop"}); len(rs) != 0 {
		t.Fatalf("deleted entry found by search: %v", titles(rs))
	}
	if err := s.Delete(ctx, "shop", res.ID, "gabi"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if st, _ := s.Stats(ctx); st.Deleted != 1 || st.Observations != 0 {
		t.Fatalf("soft delete keeps the row: %+v", st)
	}
}

func searchRelevance(t *testing.T, s store.Store) {
	ctx := context.Background()
	mustSave(t, s, entry("shop", "gabi", "Logging format", "we mention the authentication flow once in passing"))
	mustSave(t, s, entry("shop", "gabi", "Authentication flow", "tokens rotate every hour"))
	mustSave(t, s, entry("shop", "gabi", "Database naming", "snake_case everywhere"))
	rs, err := s.Search(ctx, memory.Query{Text: "authentication", Project: "shop"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].Title != "Authentication flow" {
		t.Fatalf("a title match must outrank a body mention: %v", titles(rs))
	}
	if rs[0].Score <= rs[1].Score {
		t.Fatalf("scores must reflect the ranking: %v vs %v", rs[0].Score, rs[1].Score)
	}
	// Prefix matching finds inflected forms without stemming.
	if rs, _ = s.Search(ctx, memory.Query{Text: "authent", Project: "shop"}); len(rs) != 2 {
		t.Fatalf("prefix search: %v", titles(rs))
	}
	// Spanish text and accents work with the language-neutral configuration.
	mustSave(t, s, entry("shop", "gabi", "Sesión de usuarios", "la autenticación usa cookies firmadas"))
	if rs, _ = s.Search(ctx, memory.Query{Text: "sesión", Project: "shop"}); len(rs) != 1 || rs[0].Title != "Sesión de usuarios" {
		t.Fatalf("spanish search: %v", titles(rs))
	}
	// Case-insensitive; snake_case words are searchable by their parts.
	if rs, _ = s.Search(ctx, memory.Query{Text: "SNAKE", Project: "shop"}); len(rs) != 1 {
		t.Fatalf("case/parts: %v", titles(rs))
	}
}

func searchFilters(t *testing.T, s store.Store) {
	ctx := context.Background()
	for i := range 30 {
		e := entry("shop", "gabi", fmt.Sprintf("Convention number %d", i), "common words here")
		e.Type = memory.TypeConvention
		mustSave(t, s, e)
	}
	mustSave(t, s, entry("shop", "gabi", "Pending convention", "alpha beta"))
	mustSave(t, s, entry("other", "gonza", "Convention elsewhere", "common words here"))
	if rs, _ := s.Search(ctx, memory.Query{Text: "convention", Project: "shop"}); len(rs) != store.DefaultLimit {
		t.Fatalf("default limit: %d", len(rs))
	}
	if rs, _ := s.Search(ctx, memory.Query{Text: "convention", Project: "shop", Limit: 5}); len(rs) != 5 {
		t.Fatalf("explicit limit: %d", len(rs))
	}
	if rs, _ := s.Search(ctx, memory.Query{Text: "convention", Project: "shop", Limit: 10_000}); len(rs) != 31 {
		t.Fatalf("limit must never exceed the maximum: %d", len(rs))
	}
	for _, r := range mustSearch(t, s, memory.Query{Text: "convention", Project: "shop", Limit: 50}) {
		if r.Project != "shop" {
			t.Fatalf("project isolation broken: %+v", r.Entry)
		}
	}
	if rs := mustSearch(t, s, memory.Query{Text: "convention", Project: "shop", Type: memory.TypeDecision}); len(rs) != 1 || rs[0].Title != "Pending convention" {
		t.Fatalf("type filter: %v", titles(rs))
	}
	if rs := mustSearch(t, s, memory.Query{Text: "alpha gamma", Project: "shop"}); len(rs) != 0 {
		t.Fatalf("all-terms search must need every term: %v", titles(rs))
	}
	if rs := mustSearch(t, s, memory.Query{Text: "alpha gamma", Project: "shop", MatchAny: true}); len(rs) != 1 {
		t.Fatalf("match-any: %v", titles(rs))
	}
	if rs := mustSearch(t, s, memory.Query{Project: "shop", Type: memory.TypeDecision}); len(rs) != 1 {
		t.Fatalf("no text lists by recency within the filters: %v", titles(rs))
	}
}

func mustSearch(t *testing.T, s store.Store, q memory.Query) []store.Record {
	t.Helper()
	rs, err := s.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search %+v: %v", q, err)
	}
	return rs
}

func searchSafe(t *testing.T, s store.Store) {
	mustSave(t, s, entry("shop", "gabi", "Plain entry", "nothing special"))
	for _, text := range []string{
		`x'; DROP TABLE observations; --`, `a & b | !c`, `((( ))) :* <->`, `\`, `"`, `'`, "   ", "ñandú ünï", `plain:*`, strings.Repeat("word ", 500),
	} {
		if _, err := s.Search(context.Background(), memory.Query{Text: text, Project: "shop", MatchAny: true}); err != nil {
			t.Fatalf("search %q failed: %v", text, err)
		}
	}
	if rs := mustSearch(t, s, memory.Query{Text: "plain", Project: "shop"}); len(rs) != 1 {
		t.Fatal("the table must still be there")
	}
}

func active(t *testing.T, s store.Store) {
	ctx := context.Background()
	a := entry("shop", "gabi", "Login form", "wip")
	a.Task, a.Type = "auth-login", memory.TypeProgress
	b := entry("shop", "gonza", "Validation", "wip")
	b.Task, b.Type = "user-validation", memory.TypeProgress
	mustSave(t, s, a)
	time.Sleep(15 * time.Millisecond)
	cut := time.Now()
	time.Sleep(15 * time.Millisecond)
	mustSave(t, s, b)
	mustSave(t, s, entry("other", "gabi", "Elsewhere", "wip"))
	got, err := s.Active(ctx, store.ActiveQuery{Project: "shop"})
	if err != nil || len(got) != 2 || got[0].Title != "Validation" {
		t.Fatalf("newest first: %v %v", titles(got), err)
	}
	if got, _ = s.Active(ctx, store.ActiveQuery{Project: "shop", Actor: "gabi"}); len(got) != 1 || got[0].Task != "auth-login" {
		t.Fatalf("actor filter: %v", titles(got))
	}
	if got, _ = s.Active(ctx, store.ActiveQuery{Project: "shop", Task: "user-validation"}); len(got) != 1 || got[0].Actor != "gonza" {
		t.Fatalf("task filter: %v", titles(got))
	}
	if got, _ = s.Active(ctx, store.ActiveQuery{Project: "shop", Since: cut}); len(got) != 1 || got[0].Title != "Validation" {
		t.Fatalf("since filter: %v", titles(got))
	}
	if got, _ = s.Active(ctx, store.ActiveQuery{Project: "shop", Limit: 1}); len(got) != 1 {
		t.Fatalf("limit: %v", titles(got))
	}
}

func summaries(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.SaveSummary(ctx, memory.Summary{Project: "shop"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("actor required: %v", err)
	}
	if err := s.SaveSummary(ctx, memory.Summary{Actor: "gabi"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("project required: %v", err)
	}
	for i := range 3 {
		sum := memory.Summary{Project: "shop", Actor: "gabi", Task: fmt.Sprintf("t%d", i), Goal: "g", Accomplished: "done", NextSteps: "more", Files: []string{"a.go"}}
		if err := s.SaveSummary(ctx, sum); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := s.SaveSummary(ctx, memory.Summary{Project: "other", Actor: "gonza", Goal: "x"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Summaries(ctx, "shop", 2)
	if err != nil || len(got) != 2 || got[0].Task != "t2" || got[1].Task != "t1" || got[0].Actor != "gabi" || len(got[0].Files) != 1 || got[0].Timestamp.IsZero() {
		t.Fatalf("newest first and capped: %+v %v", got, err)
	}
	if st, _ := s.Stats(ctx); st.Summaries != 4 {
		t.Fatalf("stats: %+v", st)
	}
}

func tokens(t *testing.T, s store.Store) {
	ctx := context.Background()
	rec := store.TokenRecord{ID: "abc123", User: "gabi", Hash: []byte{1, 2, 3}}
	if err := s.CreateToken(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateToken(ctx, rec); err == nil {
		t.Fatal("duplicate id must fail")
	}
	if err := s.CreateToken(ctx, store.TokenRecord{ID: "def456", User: "gonza", Hash: []byte{9}, Projects: []string{"shop", "kraken"}}); err != nil {
		t.Fatal(err)
	}
	if sc, _ := s.TokenByID(ctx, "def456"); sc.AllProjects || !sc.Allows("shop") || !sc.Allows("kraken") || sc.Allows("other") || len(sc.Projects) != 2 {
		t.Fatalf("scoped token must round-trip its allowlist: %+v", sc)
	}
	if sc, _ := s.TokenByID(ctx, "abc123"); sc.AllProjects || sc.Allows("shop") {
		t.Fatalf("a token with no scope fields allows nothing: %+v", sc)
	}
	if err := s.CreateToken(ctx, store.TokenRecord{ID: "ghi789", User: "pepe", Hash: []byte{7}, AllProjects: true}); err != nil {
		t.Fatal(err)
	}
	if sc, _ := s.TokenByID(ctx, "ghi789"); !sc.AllProjects || !sc.Allows("anything") {
		t.Fatalf("all-project token: %+v", sc)
	}
	if l, _ := s.ListTokens(ctx, "gonza"); len(l) != 1 || len(l[0].Projects) != 2 {
		t.Fatalf("list must carry the scope: %+v", l)
	}
	got, err := s.TokenByID(ctx, "abc123")
	if err != nil || got.User != "gabi" || string(got.Hash) != "\x01\x02\x03" || !got.Active() || got.CreatedAt.IsZero() {
		t.Fatalf("by id: %+v %v", got, err)
	}
	if _, err := s.TokenByID(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown token: %v", err)
	}
	if l, _ := s.ListTokens(ctx, "gabi"); len(l) != 1 || l[0].ID != "abc123" || len(l[0].Hash) != 0 {
		t.Fatalf("list must filter by user and never expose the hash: %+v", l)
	}
	if l, _ := s.ListTokens(ctx, ""); len(l) != 3 {
		t.Fatalf("list all: %+v", l)
	}
	if err := s.RevokeToken(ctx, "gonza", "abc123"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoking another user's token: %v", err)
	}
	if err := s.RevokeToken(ctx, "gabi", "abc123"); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.TokenByID(ctx, "abc123"); got.Active() || got.RevokedAt.IsZero() {
		t.Fatalf("revoked: %+v", got)
	}
	if err := s.RevokeToken(ctx, "gabi", "abc123"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second revoke: %v", err)
	}
	if st, _ := s.Stats(ctx); st.Tokens != 2 {
		t.Fatalf("active tokens: %+v", st)
	}
}

func doctorStats(t *testing.T, s store.Store) {
	ctx := context.Background()
	mustSave(t, s, entry("shop", "gabi", "One", "x"))
	mustSave(t, s, entry("shop", "gonza", "Two", "x"))
	mustSave(t, s, entry("other", "gonza", "Three", "x"))
	st, err := s.Stats(ctx)
	if err != nil || st.Observations != 3 || st.Projects != 2 || st.Actors != 2 {
		t.Fatalf("stats: %+v %v", st, err)
	}
	rep := s.Doctor(ctx)
	if !rep.OK() || len(rep.Checks) == 0 {
		t.Fatalf("doctor: %+v", rep)
	}
	for _, c := range rep.Checks {
		if strings.Contains(c.Message, "://") || strings.Contains(strings.ToLower(c.Message), "password") {
			t.Fatalf("doctor leaks connection details: %+v", c)
		}
	}
}
