package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/budget"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
)

// maxHookInput bounds what the hook reads from stdin.
const maxHookInput = 1 << 20

func runContext(env Env, args []string) int {
	return subcommands(env, "context", args, map[string]func(Env, []string) int{
		"add": runContextAdd, "report": runContextReport, "check": runContextCheck,
		"checkpoint": runContextCheckpoint, "hook": runContextHook,
	})
}

// openMonitor resolves the project and loads the budget of the active person.
func openMonitor(env Env, flagRoot string) (*budget.Monitor, gabitotl.WorkflowConfig, string, error) {
	root, err := resolveRoot(env, flagRoot)
	if err != nil {
		return nil, gabitotl.WorkflowConfig{}, "", err
	}
	layout := gabitotl.NewLayout(root)
	wf, err := gabitotl.LoadWorkflow(layout)
	if err != nil {
		return nil, wf, root, fmt.Errorf("cannot load .gabitotl/workflow.md (run `gabitotl init`): %w", err)
	}
	user := gabitotl.ActiveUserID()
	if user == "" {
		user = activeID(root)
	}
	m, err := budget.New(layout, user, budget.FromWorkflow(wf))
	return m, wf, root, err
}

func ctxFlags(name string, env Env) (*flag.FlagSet, *string, *bool) {
	fs := flag.NewFlagSet("context "+name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	return fs, fs.String("root", "", "project root (default: Git top-level of the current directory)"), fs.Bool("json", false, "print JSON")
}

func ctxFail(env Env, name string, err error) int {
	fmt.Fprintf(env.Stderr, "gabitotl context %s: %v\n", name, err)
	return ExitError
}

func runContextAdd(env Env, args []string) int {
	fs, root, _ := ctxFlags("add", env)
	file := fs.String("file", "", "file whose text is added (default: stdin)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	var data []byte
	var err error
	if *file != "" {
		data, err = os.ReadFile(*file)
	} else {
		data, err = io.ReadAll(env.Stdin)
	}
	if err != nil {
		return ctxFail(env, "add", err)
	}
	m, _, _, err := openMonitor(env, *root)
	if err != nil {
		return ctxFail(env, "add", err)
	}
	if err := m.AddText(string(data)); err != nil {
		return ctxFail(env, "add", err)
	}
	return printContext(env, m, false)
}

func runContextReport(env Env, args []string) int {
	fs, root, js := ctxFlags("report", env)
	tokens := fs.Int("tokens", -1, "real token count reported by the runtime")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	if *tokens < 0 {
		fmt.Fprintln(env.Stderr, "Usage: gabitotl context report --tokens N [--root DIR] [--json]")
		return ExitUsage
	}
	m, _, _, err := openMonitor(env, *root)
	if err != nil {
		return ctxFail(env, "report", err)
	}
	if err := m.ReportTokens(*tokens); err != nil {
		return ctxFail(env, "report", err)
	}
	return printContext(env, m, *js)
}

// runContextCheck prints one line and always exits 0; unlike the hook it does
// not consume the one-shot checkpoint signal.
func runContextCheck(env Env, args []string) int {
	fs, root, js := ctxFlags("check", env)
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	m, _, _, err := openMonitor(env, *root)
	if err != nil {
		return ctxFail(env, "check", err)
	}
	return printContext(env, m, *js)
}

func printContext(env Env, m *budget.Monitor, js bool) int {
	d := m.Peek()
	if js {
		b, _ := json.Marshal(d)
		fmt.Fprintln(env.Stdout, string(b))
		return ExitOK
	}
	status := "ok"
	if d.ShouldSave {
		status = "save-now"
	}
	fmt.Fprintf(env.Stdout, "context: ~%s/%s (%.0f%%) %s\n", kTokens(d.Used), kTokens(d.Limit), d.Percent, status)
	return ExitOK
}

func kTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%dk", (n+500)/1000)
	}
	return fmt.Sprint(n)
}

func runContextCheckpoint(env Env, args []string) int {
	fs, root, _ := ctxFlags("checkpoint", env)
	progress := fs.String("progress", "Automatic checkpoint (context budget)", "what is done so far")
	next := fs.String("next", "", "what comes next")
	task := fs.String("task", "", "current task")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	m, wf, rootDir, err := openMonitor(env, *root)
	if err != nil {
		return ctxFail(env, "checkpoint", err)
	}
	p, cfg, err := openMemory(env, rootDir)
	if err != nil {
		fmt.Fprintf(env.Stdout, "context: checkpoint skipped (no memory provider: %v)\n", err)
		return ExitOK
	}
	ctx, cancel := context.WithTimeout(context.Background(), memoryTimeout)
	defer cancel()
	res, err := m.Checkpoint(ctx, memory.NewRecorder(p, wf), budget.State{
		Project: cfg.ProjectKey, Actor: activeID(rootDir), Task: *task, Branch: gitBranch(rootDir),
		Progress: *progress, NextStep: *next,
	})
	switch {
	case err != nil:
		fmt.Fprintf(env.Stdout, "context: checkpoint not saved (%v)\n", err)
	case !res.Recorded:
		fmt.Fprintln(env.Stdout, "context: checkpoint skipped (after-work memory is disabled)")
	default:
		fmt.Fprintln(env.Stdout, "context: checkpoint saved to memory")
	}
	return ExitOK
}

func gitBranch(root string) string {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// hookInput is the part of the host hook JSON this command reads.
type hookInput struct {
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
}

// TranscriptTokens measures the transcript at path. It returns the real count
// of the latest assistant usage record when the JSONL has one (source
// "reported"), otherwise an estimate from the file size ("estimated").
func TranscriptTokens(path string) (int, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, "", err
	}
	real := -1
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"usage"`) {
			continue
		}
		var rec struct {
			Message struct {
				Usage *struct {
					In    int `json:"input_tokens"`
					Read  int `json:"cache_read_input_tokens"`
					Write int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &rec) == nil && rec.Message.Usage != nil {
			if n := rec.Message.Usage.In + rec.Message.Usage.Read + rec.Message.Usage.Write; n > 0 {
				real = n
			}
		}
	}
	if real >= 0 {
		return real, budget.SourceReported, nil
	}
	return int(st.Size()+3) / 4, budget.SourceEstimated, nil
}

// hookFormat is the output flavor of a hook: Claude Code JSON or plain text.
const (
	formatClaude = "claude"
	formatText   = "text"
)

// hookFlags parses the flags shared by the hook entry points. Hooks must never
// fail the host, so parse problems are silent and reported through ok.
func hookFlags(name string, args []string) (fs *flag.FlagSet, format, event *string, dump *bool, ok bool) {
	fs = flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	format = fs.String("format", formatClaude, "output format: claude (JSON) or text")
	event = fs.String("event", "", "free label for the dump file name")
	dump = fs.Bool("dump", false, "write the raw stdin to .gabitotl/tmp for inspection")
	fs.String("root", "", "project root (accepted, unused)")
	ok = fs.Parse(args) == nil
	return
}

// emitHook prints msg in the requested format; nothing when msg is empty.
func emitHook(env Env, format, hookEvent, msg string) {
	if msg == "" {
		return
	}
	if format == formatText {
		fmt.Fprintln(env.Stdout, strings.TrimRight(msg, "\n"))
		return
	}
	b, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": hookEvent, "additionalContext": msg}})
	fmt.Fprintln(env.Stdout, string(b))
}

// stdinUsage looks for an obvious numeric usage field in hook JSON of an
// unknown schema (the Kiro payload is undocumented).
func stdinUsage(raw []byte) (int, bool) {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return 0, false
	}
	num := func(v any) int {
		if f, ok := v.(float64); ok && f > 0 {
			return int(f)
		}
		return 0
	}
	if u, ok := m["usage"].(map[string]any); ok {
		if n := num(u["input_tokens"]) + num(u["cache_read_input_tokens"]) + num(u["cache_creation_input_tokens"]); n > 0 {
			return n, true
		}
	}
	for _, k := range []string{"context_tokens", "tokens"} {
		if n := num(m[k]); n > 0 {
			return n, true
		}
	}
	return 0, false
}

const keepDumps = 5

// dumpHookInput writes raw to <cwd>/.gabitotl/tmp/hook-stdin-<event>-<ts>.json
// and keeps the newest keepDumps. Best effort: errors are ignored.
func dumpHookInput(dir, event string, raw []byte) {
	if dir == "" {
		return
	}
	event = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, event)
	if event == "" {
		event = "hook"
	}
	tmp := gabitotl.NewLayout(dir).Tmp()
	if os.MkdirAll(tmp, 0o755) != nil {
		return
	}
	name := fmt.Sprintf("hook-stdin-%s-%s.json", event, time.Now().UTC().Format("20060102T150405.000000000"))
	if os.WriteFile(filepath.Join(tmp, name), raw, 0o600) != nil {
		return
	}
	matches, _ := filepath.Glob(filepath.Join(tmp, "hook-stdin-*.json"))
	stamp := func(p string) string {
		b := strings.TrimSuffix(filepath.Base(p), ".json")
		return b[max(0, len(b)-25):]
	}
	sort.Slice(matches, func(i, j int) bool { return stamp(matches[i]) < stamp(matches[j]) })
	for len(matches) > keepDumps {
		_ = os.Remove(matches[0])
		matches = matches[1:]
	}
}

// runContextHook is the host hook entry: it reads the hook JSON, measures the
// context (transcript, else obvious usage fields), and when the threshold is
// crossed prints a tiny message. It never fails the host: every problem exits 0
// silently, and it never writes to stderr.
func runContextHook(env Env, args []string) int {
	_, format, event, dump, ok := hookFlags("context hook", args)
	if !ok {
		return ExitOK
	}
	raw, _ := io.ReadAll(io.LimitReader(env.Stdin, maxHookInput))
	var in hookInput
	_ = json.Unmarshal(raw, &in)
	if *dump {
		dir := in.Cwd
		if dir == "" && env.Getwd != nil {
			dir, _ = env.Getwd()
		}
		dumpHookInput(dir, *event, raw)
	}
	var used int
	var src string
	if in.TranscriptPath != "" {
		n, s, err := TranscriptTokens(in.TranscriptPath)
		if err != nil {
			return ExitOK
		}
		used, src = n, s
	} else if n, found := stdinUsage(raw); found {
		used, src = n, budget.SourceReported
	} else {
		return ExitOK
	}
	m, _, _, err := openMonitor(env, in.Cwd)
	if err != nil {
		return ExitOK
	}
	if m.SetUsed(used, src) != nil {
		return ExitOK
	}
	d, err := m.Check()
	if err != nil || !d.ShouldSave {
		return ExitOK
	}
	msg := fmt.Sprintf("Context is at %.0f%% of its budget. Run `gabitotl context checkpoint --progress \"...\" --next \"...\"` now. %s", d.Percent, d.Message)
	emitHook(env, *format, "UserPromptSubmit", msg)
	return ExitOK
}
