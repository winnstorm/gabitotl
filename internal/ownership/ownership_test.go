package ownership

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSectionUpsertIdempotentAndRestores(t *testing.T) {
	for _, orig := range []string{"", "# Mine\n\nrules\n", "no trailing newline", "A\n\n\nB\n"} {
		once, err := UpsertSection(orig, "persona", "hello\nworld")
		if err != nil {
			t.Fatal(err)
		}
		twice, _ := UpsertSection(once, "persona", "hello\nworld")
		if once != twice {
			t.Fatalf("not idempotent:\n%q\n%q", once, twice)
		}
		if !strings.HasPrefix(once, orig) {
			t.Fatalf("user content moved: %q", once)
		}
		changed, _ := UpsertSection(once, "persona", "new body")
		if body, _ := SectionBody(changed, "persona"); body != "new body" || !strings.HasPrefix(changed, orig) {
			t.Fatalf("replace broke content: %q", changed)
		}
		back, ok := RemoveSection(once, "persona")
		if !ok {
			t.Fatal("section not found")
		}
		want := orig
		if orig != "" && !strings.HasSuffix(orig, "\n") {
			want = orig + "\n" // newline added to terminate the user's last line
		}
		if back != want {
			t.Fatalf("remove did not restore: %q want %q", back, want)
		}
	}
}

func TestSectionKeepsOtherSectionsAndRejectsMarkerBody(t *testing.T) {
	c, _ := UpsertSection("x\n", "a", "1")
	c, _ = UpsertSection(c, "b", "2")
	c, _ = RemoveSection(c, "a")
	if _, ok := SectionBody(c, "b"); !ok || strings.Contains(c, "gabitotl:start a") {
		t.Fatalf("bad removal: %q", c)
	}
	if _, err := UpsertSection("", "a", "<!-- gabitotl:end a -->"); err == nil {
		t.Fatal("marker in body must be rejected")
	}
}

const jsonc = `{
  // my providers
  "provider": { "x": 1 }, /* keep */
  "model": "mine",
}
`

func TestSetPreservesJSONCBytes(t *testing.T) {
	out, res, err := Set([]byte(jsonc), []string{"mcp", "engram"}, json.RawMessage(`{"type":"local","command":["engram","mcp"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created {
		t.Fatal("mcp parent was created")
	}
	if !strings.HasPrefix(string(out), strings.TrimSuffix(jsonc, "\n}\n")) {
		t.Fatalf("existing content altered:\n%s", out)
	}
	back, ok, err := Remove(out, []string{"mcp", "engram"}, true)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	if string(back) != jsonc {
		t.Fatalf("remove did not restore:\n%s", back)
	}
}

func TestSetReplaceKeepsPrevious(t *testing.T) {
	out, res, err := Set([]byte(jsonc), []string{"model"}, json.RawMessage(`"gabito"`))
	if err != nil || string(res.Previous) != `"mine"` || res.Created {
		t.Fatal(err, res)
	}
	if !strings.Contains(string(out), `"model": "gabito"`) || !strings.Contains(string(out), "// my providers") {
		t.Fatalf("%s", out)
	}
}

func TestSetEmptyAndSingleLine(t *testing.T) {
	out, _, err := Set(nil, []string{"a", "b"}, json.RawMessage(`1`))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]map[string]int
	if json.Unmarshal(out, &v) != nil || v["a"]["b"] != 1 {
		t.Fatalf("%s", out)
	}
	out, _, _ = Set([]byte(`{"x":1}`), []string{"y"}, json.RawMessage(`2`))
	if string(out) != `{"x":1, "y": 2}` {
		t.Fatalf("%s", out)
	}
	back, _, _ := Remove(out, []string{"y"}, false)
	if string(back) != `{"x":1}` {
		t.Fatalf("%s", back)
	}
}

func TestUnsafeDocumentsAreRefused(t *testing.T) {
	for _, bad := range []string{`{"a": }`, `[1,2]`, `{"a": 1 "b": 2}`, `{"a": /* open`, `{"a":1} trailing`} {
		_, _, err := Set([]byte(bad), []string{"k"}, json.RawMessage(`1`))
		var ue *UnsafeError
		if !errors.Is(err, ErrUnsafe) || !errors.As(err, &ue) {
			t.Errorf("%q: want unsafe error, got %v", bad, err)
		}
	}
	// A scalar where an object is needed is also refused, never overwritten.
	if _, _, err := Set([]byte(`{"mcp": 5}`), []string{"mcp", "x"}, json.RawMessage(`1`)); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("got %v", err)
	}
}

func TestArrayAddRemove(t *testing.T) {
	src := "{\n  \"instructions\": [\n    \"CONTRIBUTING.md\"\n  ]\n}\n"
	out, added, created, err := ArrayAdd([]byte(src), []string{"instructions"}, "g.md")
	if err != nil || !added || created {
		t.Fatal(err, added, created)
	}
	again, added, _, _ := ArrayAdd(out, []string{"instructions"}, "g.md")
	if added || string(again) != string(out) {
		t.Fatal("array add not idempotent")
	}
	back, ok, err := ArrayRemove(out, []string{"instructions"}, "g.md", false)
	if err != nil || !ok || string(back) != src {
		t.Fatalf("%v %v\n%s", err, ok, back)
	}
	out, _, created, _ = ArrayAdd([]byte("{}\n"), []string{"instructions"}, "g.md")
	if !created {
		t.Fatal("array was created")
	}
	back, _, _ = ArrayRemove(out, []string{"instructions"}, "g.md", true)
	if strings.Contains(string(back), "instructions") {
		t.Fatalf("empty array not pruned: %s", back)
	}
}

func TestMembersAndGet(t *testing.T) {
	ms, err := Members([]byte(jsonc), nil)
	if err != nil || len(ms) != 2 || ms[0].Key != "provider" {
		t.Fatal(err, ms)
	}
	if raw, ok, _ := Get([]byte(jsonc), []string{"model"}); !ok || string(raw) != `"mine"` {
		t.Fatal(string(raw))
	}
}

func TestStateRoundTripAndMerge(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	st, err := s.Load()
	if err != nil || len(st.Entries) != 0 {
		t.Fatal(err)
	}
	st.Upsert(Entry{Runtimes: []string{"kiro-cli"}, Path: "/p", Keys: []Key{{Path: []string{"a"}}}, HashBefore: "h0", HashAfter: "h1", Version: "1"})
	st.Upsert(Entry{Runtimes: []string{"kiro-ide"}, Path: "/p", Keys: []Key{{Path: []string{"a"}}}, HashBefore: "zz", HashAfter: "h2", Version: "1"})
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Load()
	e := got.Entries[0]
	if len(got.Entries) != 1 || len(e.Runtimes) != 2 || len(e.Keys) != 1 || e.HashBefore != "h0" || e.HashAfter != "h2" {
		t.Fatalf("%+v", got)
	}
	if len(got.ForRuntime("kiro-ide")) != 1 || len(got.ForRuntime("claude-code")) != 0 {
		t.Fatal("ForRuntime")
	}
}

func TestSetInsertsAfterAMemberThatEndsMidLine(t *testing.T) {
	data := []byte(`{"hooks":{"A":[{"x":1}]}}`)
	for _, k := range []string{"B", "C"} {
		out, _, err := Set(data, []string{"hooks", k}, json.RawMessage(`[{"y":2}]`))
		if err != nil {
			t.Fatal(err)
		}
		data = out
		if !json.Valid(data) {
			t.Fatalf("invalid JSON after adding %s:\n%s", k, data)
		}
	}
}
