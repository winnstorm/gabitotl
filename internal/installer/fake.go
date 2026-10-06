package installer

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// FakeRunner is a Runner for tests: it records every invocation and answers
// from Handlers keyed by "name subcommand" (for example "engram doctor") or by
// "name" alone. It never executes anything.
type FakeRunner struct {
	mu       sync.Mutex
	Calls    []string
	Handlers map[string]func(args []string) (stdout string, err error)
}

// Run implements Runner.
func (f *FakeRunner) Run(_ context.Context, _ string, name string, args ...string) ([]byte, []byte, error) {
	base := name
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	f.mu.Lock()
	f.Calls = append(f.Calls, strings.TrimSpace(base+" "+strings.Join(args, " ")))
	f.mu.Unlock()
	if len(args) > 0 {
		if h, ok := f.Handlers[base+" "+args[0]]; ok {
			out, err := h(args)
			return []byte(out), nil, err
		}
	}
	if h, ok := f.Handlers[base]; ok {
		out, err := h(args)
		return []byte(out), nil, err
	}
	return nil, nil, fmt.Errorf("fake runner: no handler for %q", base)
}

// Count returns how many recorded calls start with prefix.
func (f *FakeRunner) Count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// Log returns a copy of the recorded invocations in order.
func (f *FakeRunner) Log() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.Calls...)
}
