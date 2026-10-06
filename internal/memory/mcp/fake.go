package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/winnstorm/gabitotl/internal/memory"
)

// FakeTransport is an in-memory Transport for tests. It honors the wire
// contract in the package documentation using DefaultTools names.
type FakeTransport struct {
	mu        sync.Mutex
	Entries   []memory.Entry
	Summaries []memory.Summary
	Calls     []string
	Project   string
	// Fail makes every call return this error when set.
	Fail error
}

// Call implements Transport.
func (f *FakeTransport) Call(_ context.Context, tool string, args any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, tool)
	if f.Fail != nil {
		return nil, f.Fail
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	d := DefaultTools()
	switch tool {
	case d.Save:
		var e memory.Entry
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		e.ID = memory.ID(strconv.Itoa(len(f.Entries) + 1))
		f.Entries = append(f.Entries, e)
		return json.Marshal(map[string]any{"id": e.ID})
	case d.Search:
		var q memory.Query
		if err := json.Unmarshal(raw, &q); err != nil {
			return nil, err
		}
		var out []memory.Entry
		for _, e := range f.Entries {
			if matches(e, q) {
				out = append(out, e)
			}
		}
		return json.Marshal(map[string]any{"entries": out})
	case d.Update:
		var req struct {
			ID    memory.ID         `json:"id"`
			Patch memory.EntryPatch `json:"patch"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		for i := range f.Entries {
			if f.Entries[i].ID == req.ID {
				if req.Patch.Title != nil {
					f.Entries[i].Title = *req.Patch.Title
				}
				if req.Patch.Content != nil {
					f.Entries[i].Content = *req.Patch.Content
				}
				return json.RawMessage(`{}`), nil
			}
		}
		return nil, fmt.Errorf("entry %s not found", req.ID)
	case d.SessionSummary:
		var s memory.Summary
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		f.Summaries = append(f.Summaries, s)
		return json.RawMessage(`{}`), nil
	case d.CurrentProject:
		return json.Marshal(map[string]any{"project": f.Project})
	case d.Health:
		return json.Marshal(map[string]any{"provider": "fake", "ok": true,
			"checks": []memory.Check{{Name: "store", Status: memory.StatusOK, Message: "fake store"}}})
	case d.ActiveWork:
		var q ActiveQuery
		if err := json.Unmarshal(raw, &q); err != nil {
			return nil, err
		}
		var out []memory.Entry
		for _, e := range f.Entries {
			if (q.Project == "" || e.Project == q.Project) && (q.Actor == "" || e.Actor == q.Actor) && (q.Task == "" || e.Task == q.Task) {
				out = append(out, e)
			}
		}
		return json.Marshal(map[string]any{"entries": out})
	}
	return nil, fmt.Errorf("unknown tool %q", tool)
}

func matches(e memory.Entry, q memory.Query) bool {
	if q.Project != "" && e.Project != q.Project {
		return false
	}
	if q.Type != "" && e.Type != q.Type {
		return false
	}
	hay := strings.ToLower(e.Title + " " + e.Content)
	any, all := false, true
	for _, t := range strings.Fields(strings.ToLower(q.Text)) {
		if strings.Contains(hay, t) {
			any = true
		} else {
			all = false
		}
	}
	if q.MatchAny {
		return any
	}
	return all
}
