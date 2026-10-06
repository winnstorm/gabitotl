package gabitotl

import (
	"os"
	"path/filepath"
	"strings"
)

// InitOptions controls Init. Zero values pick defaults.
type InitOptions struct {
	Name        string // default: base name of the root
	Kind        string // new|existing; default: DetectKind(root)
	Mode        string // solo|team; default solo
	Provider    string // memory provider; default engram (solo) or memory-mcp (team)
	Runtime     string // default "unset" until an adapter installs one
	VCSProvider string // default git
}

// InitResult lists what Init created and what already existed.
type InitResult struct {
	Created []string // paths relative to the root
	Skipped []string
}

// Exists reports whether root already contains a .gabitotl directory.
func Exists(root string) bool {
	st, err := os.Stat(NewLayout(root).Dir())
	return err == nil && st.IsDir()
}

// Init creates .gabitotl/ with default templates. It is idempotent: existing
// files are never touched. It does not create local identity files.
func Init(root string, opts InitOptions) (InitResult, error) {
	var res InitResult
	root, err := filepath.Abs(root)
	if err != nil {
		return res, err
	}
	if err := ValidateRoot(root); err != nil {
		return res, err
	}
	l := NewLayout(root)

	name := opts.Name
	if name == "" {
		name = filepath.Base(root)
	}
	kind := opts.Kind
	if kind == "" {
		if kind, err = DetectKind(root); err != nil {
			return res, err
		}
	}
	mode := orDefault(opts.Mode, ModeSolo)
	provider := opts.Provider
	if provider == "" {
		provider = MemoryEngram
		if mode == ModeTeam {
			provider = MemoryMemoryMCP
		}
	}
	mem := MemoryConfig{Mode: mode, Provider: provider, ProjectKey: orDefault(Slug(name), "project")}
	if provider == MemoryMemoryMCP {
		mem.Backend = "postgresql"
		mem.PostgresConnection = "env:GABITOTL_MEMORY_DATABASE_URL"
	} else {
		mem.Backend = "local"
	}
	proj := DefaultProject(name, kind)
	rt := RuntimeConfig{Runtime: orDefault(opts.Runtime, "unset"), AllowModelSwitch: true}
	vcs := DefaultVCS(orDefault(opts.VCSProvider, "git"))
	wf := DefaultWorkflow()
	for _, v := range []interface{ Validate() error }{&proj, &mem, &rt, &vcs, &wf} {
		if err := v.Validate(); err != nil {
			return res, err
		}
	}

	files := []struct {
		path  string
		front any
		body  string
	}{
		{l.Project(), proj, bodyProject},
		{l.Memory(), mem, bodyMemory},
		{l.Runtime(), rt, bodyRuntime},
		{l.VCS(), vcs, bodyVCS},
		{l.Workflow(), wf, bodyWorkflow},
	}
	for _, f := range files {
		data, err := RenderDoc(f.front, f.body)
		if err != nil {
			return res, err
		}
		if err := res.write(root, f.path, data); err != nil {
			return res, err
		}
	}
	imports := []byte(fence + "\n" + importsFront + fence + "\n" + bodyImports)
	if err := res.write(root, l.Imports(), imports); err != nil {
		return res, err
	}
	gitignore := []byte(strings.Join(GitignoreEntries, "\n") + "\n")
	if err := res.write(root, l.Gitignore(), gitignore); err != nil {
		return res, err
	}
	if err := res.write(root, filepath.Join(l.Work(), ".gitkeep"), nil); err != nil {
		return res, err
	}
	if err := os.MkdirAll(l.Users(), 0o755); err != nil {
		return res, err
	}
	if err := os.MkdirAll(l.Local(), 0o755); err != nil {
		return res, err
	}
	return res, nil
}

func (r *InitResult) write(root, path string, data []byte) error {
	created, err := WriteFileIfMissing(path, data)
	if err != nil {
		return err
	}
	rel, relErr := filepath.Rel(root, path)
	if relErr != nil {
		return relErr
	}
	if created {
		r.Created = append(r.Created, rel)
	} else {
		r.Skipped = append(r.Skipped, rel)
	}
	return nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
