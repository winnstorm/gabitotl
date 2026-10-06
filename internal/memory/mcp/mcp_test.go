package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
)

func newProv(t *testing.T, ft *FakeTransport, mut func(*Options)) *Provider {
	t.Helper()
	o := Options{Transport: ft, Project: "proj", ConnectionRef: "env:GABITOTL_MEMORY_DATABASE_URL",
		Getenv: func(k string) string {
			if k == "GABITOTL_MEMORY_DATABASE_URL" {
				return "postgres://secret"
			}
			return ""
		}}
	if mut != nil {
		mut(&o)
	}
	p, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRoundTripOverFakeTransport(t *testing.T) {
	ft := &FakeTransport{Project: "proj"}
	p := newProv(t, ft, nil)
	ctx := context.Background()
	id, err := p.Save(ctx, memory.Entry{Type: memory.TypeDecision, Title: "Use JWT", Content: "auth tokens", Actor: "gabi"})
	if err != nil || id == "" {
		t.Fatalf("save: %v", err)
	}
	got, err := p.Search(ctx, memory.Query{Text: "jwt", MatchAny: true})
	if err != nil || len(got) != 1 || got[0].Actor != "gabi" || got[0].Project != "proj" {
		t.Fatalf("search: %+v err=%v", got, err)
	}
	title := "Use JWT v2"
	if err := p.Update(ctx, id, memory.EntryPatch{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if ft.Entries[0].Title != title {
		t.Fatal("update not applied")
	}
	if err := p.SessionSummary(ctx, memory.Summary{Accomplished: "x"}); err != nil || len(ft.Summaries) != 1 {
		t.Fatalf("summary: %v", err)
	}
	if pr, err := p.CurrentProject(ctx); err != nil || pr != "proj" {
		t.Fatalf("project: %q %v", pr, err)
	}
	if err := p.Update(ctx, "", memory.EntryPatch{}); err == nil {
		t.Fatal("empty id must fail")
	}
}

func TestConfigurableToolMapping(t *testing.T) {
	ft := &FakeTransport{}
	p := newProv(t, ft, func(o *Options) { o.Tools = Tools{Search: "custom_search"} })
	_, err := p.Search(context.Background(), memory.Query{Text: "x"})
	if err == nil || ft.Calls[0] != "custom_search" {
		t.Fatalf("custom tool name not used: calls=%v err=%v", ft.Calls, err)
	}
}

func TestConnectionByReferenceOnly(t *testing.T) {
	if _, err := New(Options{Transport: &FakeTransport{}, ConnectionRef: "postgres://u:p@h/db"}); err == nil {
		t.Fatal("literal connection string must be rejected")
	}
	if _, err := ResolveRef("env:NOPE", func(string) string { return "" }); err == nil {
		t.Fatal("unset variable must error")
	}
	v, err := ResolveRef("env:X", func(string) string { return "val" })
	if err != nil || v != "val" {
		t.Fatalf("resolve: %q %v", v, err)
	}
}

func TestDoctorNeverLeaksSecret(t *testing.T) {
	ft := &FakeTransport{}
	rep := newProv(t, ft, nil).Doctor(context.Background())
	if !rep.OK() {
		t.Fatalf("report: %+v", rep)
	}
	for _, c := range rep.Checks {
		if c.Message == "postgres://secret" || strings.Contains(c.Message, "secret") {
			t.Fatalf("secret leaked: %+v", c)
		}
	}
	bad := newProv(t, &FakeTransport{Fail: errors.New("down")}, func(o *Options) { o.Getenv = func(string) string { return "" } }).Doctor(context.Background())
	if bad.OK() {
		t.Fatal("unresolved ref and failing server must be errors")
	}
}

func TestFromConfigWithoutServer(t *testing.T) {
	_, err := FromConfig(gabitotl.MemoryConfig{Provider: gabitotl.MemoryMemoryMCP, PostgresConnection: "env:X"})
	if !errors.Is(err, ErrNoTransport) {
		t.Fatalf("want ErrNoTransport, got %v", err)
	}
	if _, err := New(Options{}); !errors.Is(err, ErrNoTransport) {
		t.Fatal("nil transport must be ErrNoTransport")
	}
}
