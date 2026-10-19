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
	"strings"

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

// runContextHook is the host hook entry: it reads the hook JSON, measures the
// transcript, and when the threshold is crossed prints a tiny additionalContext
// message. It never fails the host: every problem exits 0 silently.
func runContextHook(env Env, args []string) int {
	raw, _ := io.ReadAll(io.LimitReader(env.Stdin, maxHookInput))
	var in hookInput
	if json.Unmarshal(raw, &in) != nil || in.TranscriptPath == "" {
		return ExitOK
	}
	used, src, err := TranscriptTokens(in.TranscriptPath)
	if err != nil {
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
	b, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "UserPromptSubmit", "additionalContext": msg}})
	fmt.Fprintln(env.Stdout, string(b))
	return ExitOK
}
