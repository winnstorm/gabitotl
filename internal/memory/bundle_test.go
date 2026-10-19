package memory

import (
	"strconv"
	"strings"
	"testing"
)

func TestPackBundleEmpty(t *testing.T) {
	b := PackBundle(nil, 0, 0)
	if len(b.Items) != 0 || b.Omitted != 0 || b.Summary() != "memory: 0 entries, ~0 tokens (0 omitted)" {
		t.Fatalf("%+v", b)
	}
}

func TestPackBundleTruncatesAndOrders(t *testing.T) {
	long := strings.Repeat("First sentence here. ", 200)
	es := []Entry{
		{ID: "1", Type: TypeDecision, Title: "Use JWT", Content: long},
		{ID: "2", Type: TypeBugfix, Title: "Fix race", Content: "short body"},
	}
	b := PackBundle(es, 5000, 100)
	if len(b.Items) != 2 || b.Items[0].ID != "1" || b.Items[1].ID != "2" {
		t.Fatalf("order: %+v", b.Items)
	}
	if got := estimateTokens(b.Items[0].text()); got > 100 {
		t.Fatalf("per-entry cap exceeded: %d", got)
	}
	if !strings.HasSuffix(b.Items[0].Body, ".") || b.Items[1].Body != "short body" {
		t.Fatalf("bodies: %q | %q", b.Items[0].Body, b.Items[1].Body)
	}
	if !strings.Contains(b.Render(), "decision · Use JWT · #1\n") {
		t.Fatalf("render: %s", b.Render())
	}
}

func TestPackBundleTotalCapAndOmitted(t *testing.T) {
	body := strings.Repeat("word ", 300)
	var es []Entry
	for _, id := range []ID{"1", "2", "3", "4", "5"} {
		es = append(es, Entry{ID: id, Type: TypeProgress, Title: "t", Content: body})
	}
	b := PackBundle(es, 300, 120)
	if b.Tokens > 300 || len(b.Items) == 0 || len(b.Items) == 5 {
		t.Fatalf("cap: %+v", b)
	}
	if b.Omitted != 5-len(b.Items) {
		t.Fatalf("omitted %d vs %d items", b.Omitted, len(b.Items))
	}
	if !strings.HasSuffix(b.Render(), "("+strconv.Itoa(b.Omitted)+" omitted)") {
		t.Fatalf("summary: %s", b.Render())
	}
}
