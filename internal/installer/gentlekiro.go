package installer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// GentleKiroRuntime labels the snapshot entries of Gentle AI's files in Kiro.
// They are ordinary manifest files, so `gabitotl restore <id>` and the rollback
// of a failed run put them back byte for byte.
const GentleKiroRuntime = "gentle-kiro"

// gentleKiroDir is the snapshot subfolder that holds the copies.
const gentleKiroDir = "gentle-kiro"

// ErrGentleKiroBackup means a file to retire could not be copied into the
// snapshot (or the copy did not match). Nothing was removed.
var ErrGentleKiroBackup = errors.New("backing up Gentle AI's Kiro files failed; nothing was removed")

// What Gentle AI writes into a Kiro tree. Only these names and the author marker
// of a skill prove ownership; everything else in the tree is the user's.
const (
	gentleKiroSteering = "gentle-ai.md"
	gentleKiroManifest = ".gentle-ai-native-agent-ownership.json"
	gentleKiroAuthor   = "gentleman-programming"
)

var (
	gentleKiroAgents = []string{
		"sdd-apply", "sdd-archive", "sdd-design", "sdd-explore", "sdd-init", "sdd-onboard", "sdd-propose",
		"sdd-research", "sdd-spec", "sdd-tasks", "sdd-verify", "jd-fix-agent", "jd-judge-a", "jd-judge-b",
		"review-readability", "review-refuter", "review-reliability", "review-resilience", "review-risk",
	}
	gentleKiroShared = []string{
		"README.md", "engram-convention.md", "openspec-convention.md", "persistence-contract.md",
		"research-lifecycle.md", "review-ledger-contract-pi.md", "review-ledger-contract.md",
		"sdd-phase-common.md", "sdd-status-contract.md", "skill-resolver.md",
	}
)

// GentleKiroFile is one file Gentle AI owns, with the identity captured when the
// plan was built (a file that changed since is never removed).
type GentleKiroFile struct {
	Path   string
	Rel    string // relative to the Kiro base
	Size   int64
	SHA256 string
}

// GentleKiroSkill is one skill directory whose front matter names Gentle AI.
type GentleKiroSkill struct {
	Name  string
	Files []GentleKiroFile
}

// GentleKiroBase is Gentle AI's footprint under one Kiro directory.
type GentleKiroBase struct {
	Root     string
	Steering *GentleKiroFile
	Skills   []GentleKiroSkill
	Shared   []GentleKiroFile
	Agents   []GentleKiroFile
	// Kept are files inside Gentle's directories that are not provably Gentle's.
	// They are reported and never touched.
	Kept []string
}

// GentleKiroInventory is everything Gentle AI left in the Kiro trees.
type GentleKiroInventory struct {
	Bases []GentleKiroBase
}

// GentleKiroCounts summarizes an inventory for presenters.
type GentleKiroCounts struct {
	Steering, Skills, Shared, Agents, Files, Kept int
}

// Counts totals the inventory.
func (g GentleKiroInventory) Counts() GentleKiroCounts {
	var c GentleKiroCounts
	for _, b := range g.Bases {
		if b.Steering != nil {
			c.Steering++
		}
		c.Skills += len(b.Skills)
		c.Shared += len(b.Shared)
		c.Agents += len(b.Agents)
		c.Kept += len(b.Kept)
	}
	c.Files = len(g.Files())
	return c
}

// Empty reports whether there is nothing to retire.
func (g GentleKiroInventory) Empty() bool { return len(g.Files()) == 0 }

// Files lists every file to retire, base by base.
func (g GentleKiroInventory) Files() []GentleKiroFile {
	var out []GentleKiroFile
	for _, b := range g.Bases {
		out = append(out, b.files()...)
	}
	return out
}

func (b GentleKiroBase) files() []GentleKiroFile {
	var out []GentleKiroFile
	if b.Steering != nil {
		out = append(out, *b.Steering)
	}
	for _, s := range b.Skills {
		out = append(out, s.Files...)
	}
	out = append(out, b.Shared...)
	return append(out, b.Agents...)
}

// KeptFiles lists the user-owned leftovers found inside Gentle's directories.
func (g GentleKiroInventory) KeptFiles() []string {
	var out []string
	for _, b := range g.Bases {
		out = append(out, b.Kept...)
	}
	return out
}

// GentleKiroResult reports what the retirement did, for presenters.
type GentleKiroResult struct {
	Snapshot string
	Removed  []string
	Skipped  []string // changed since planning, or could not be removed: still in place
	Kept     []string // user-owned files inside Gentle's directories
}

// foreignAssetRoots is implemented by adapters whose runtime tree may hold the
// assets of another harness. The installer never branches on runtime names.
type foreignAssetRoots interface {
	ForeignAssetRoots(env rt.Env) []string
}

// GentleKiroRoots lists the existing Kiro directories of the given runtimes.
func GentleKiroRoots(env Env, runtimes []string) []string {
	var out []string
	for _, n := range runtimes {
		a, ok := env.Runtimes.Get(n)
		if !ok {
			continue
		}
		if f, ok := a.(foreignAssetRoots); ok {
			for _, r := range f.ForeignAssetRoots(env.RT()) {
				if r = filepath.Clean(r); !slices.Contains(out, r) {
					out = append(out, r)
				}
			}
		}
	}
	return out
}

// gentleKiroCoexists reports whether Gentle AI's files still sit in a Kiro tree
// that also holds GabitoTL's own steering file: two always-on instruction sets.
// It is read-only; doctor warns about it and names the command that fixes it.
func gentleKiroCoexists(env Env, runtime string) bool {
	for _, root := range GentleKiroRoots(env, []string{runtime}) {
		if _, err := os.Stat(filepath.Join(root, "steering", "gabitotl.md")); err != nil {
			continue
		}
		if !DetectGentleKiro([]string{root}).Empty() {
			return true
		}
	}
	return false
}

// DetectGentleKiro is read-only: it inventories Gentle AI's files under roots.
func DetectGentleKiro(roots []string) GentleKiroInventory {
	var inv GentleKiroInventory
	for _, root := range roots {
		if b := detectGentleKiroBase(root); b.Steering != nil || len(b.Skills) > 0 || len(b.Shared) > 0 || len(b.Agents) > 0 {
			inv.Bases = append(inv.Bases, b)
		}
	}
	return inv
}

func detectGentleKiroBase(root string) GentleKiroBase {
	b := GentleKiroBase{Root: root}
	file := func(p string) (GentleKiroFile, bool) {
		st, err := os.Lstat(p)
		if err != nil || !st.Mode().IsRegular() {
			return GentleKiroFile{}, false
		}
		data, err := os.ReadFile(p)
		if err != nil {
			b.Kept = append(b.Kept, p) // unreadable: it cannot be backed up, so it stays
			return GentleKiroFile{}, false
		}
		rel, _ := filepath.Rel(root, p)
		return GentleKiroFile{Path: p, Rel: rel, Size: int64(len(data)), SHA256: sha(data)}, true
	}

	if f, ok := file(filepath.Join(root, "steering", gentleKiroSteering)); ok {
		b.Steering = &f
	}

	skills := filepath.Join(root, "skills")
	if es, err := os.ReadDir(skills); err == nil {
		for _, e := range es {
			if !e.IsDir() || e.Name() == "_shared" {
				continue
			}
			dir := filepath.Join(skills, e.Name())
			md, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
			if err != nil || skillAuthor(md) != gentleKiroAuthor {
				continue
			}
			s := GentleKiroSkill{Name: e.Name()}
			_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				rel, _ := filepath.Rel(dir, p)
				if f, ok := file(p); ok && isSkillFile(rel) {
					s.Files = append(s.Files, f)
				} else if !slices.Contains(b.Kept, p) {
					b.Kept = append(b.Kept, p) // not provably Gentle's, or unreadable
				}
				return nil
			})
			if len(s.Files) > 0 {
				b.Skills = append(b.Skills, s)
			}
		}
		for _, n := range gentleKiroShared {
			if f, ok := file(filepath.Join(skills, "_shared", n)); ok {
				b.Shared = append(b.Shared, f)
			}
		}
		if es, err := os.ReadDir(filepath.Join(skills, "_shared")); err == nil && len(b.Shared) > 0 {
			for _, e := range es {
				if p := filepath.Join(skills, "_shared", e.Name()); !slices.ContainsFunc(b.Shared, func(f GentleKiroFile) bool { return f.Path == p }) {
					b.Kept = append(b.Kept, p)
				}
			}
		}
	}

	agents := filepath.Join(root, "agents")
	names := map[string]bool{}
	for _, n := range gentleKiroAgents {
		names[n+".md"] = true
	}
	if data, err := os.ReadFile(filepath.Join(agents, gentleKiroManifest)); err == nil {
		names[gentleKiroManifest] = true
		for n := range ownershipFiles(data) {
			names[n] = true
		}
	}
	list := make([]string, 0, len(names))
	for n := range names {
		list = append(list, n)
	}
	sort.Strings(list)
	for _, n := range list {
		if f, ok := file(filepath.Join(agents, n)); ok {
			b.Agents = append(b.Agents, f)
		}
	}

	sort.Slice(b.Skills, func(i, j int) bool { return b.Skills[i].Name < b.Skills[j].Name })
	sort.Strings(b.Kept)
	return b
}

// ownershipFiles reads the agent names from Gentle AI's ownership manifest. Only
// plain file names count: a key with a separator never points outside agents/.
func ownershipFiles(data []byte) map[string]bool {
	out := map[string]bool{}
	var m struct {
		Files map[string]string `json:"files"`
	}
	if json.Unmarshal(data, &m) != nil {
		return out
	}
	for n := range m.Files {
		if n != "" && n == filepath.Base(n) && !strings.ContainsAny(n, `/\`) && n != "." && n != ".." {
			out[n] = true
		}
	}
	return out
}

// isSkillFile says whether a file inside a Gentle skill directory is part of the
// skill: Markdown at the top or anywhere below references/. Anything else was
// added by the user and stays.
func isSkillFile(rel string) bool {
	if !strings.EqualFold(filepath.Ext(rel), ".md") {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return len(parts) == 1 || parts[0] == "references"
}

// skillAuthor returns the lower-cased metadata.author of a SKILL.md front
// matter, or "" when the file has none or cannot be parsed. BOM, CRLF, quotes
// and trailing comments are tolerated.
func skillAuthor(b []byte) string {
	text := strings.ReplaceAll(strings.TrimPrefix(string(b), "\xef\xbb\xbf"), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	inMeta := false
	for _, ln := range lines[1:] {
		if strings.TrimSpace(ln) == "---" {
			return ""
		}
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if ln[0] != ' ' && ln[0] != '\t' {
			inMeta = strings.HasPrefix(trimmed, "metadata:") && strings.TrimSpace(strings.TrimPrefix(trimmed, "metadata:")) == ""
			continue
		}
		if inMeta {
			if v, ok := strings.CutPrefix(trimmed, "author:"); ok {
				return strings.ToLower(unquote(v))
			}
		}
	}
	return ""
}

func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') {
		if end := strings.IndexByte(v[1:], v[0]); end >= 0 {
			return v[1 : 1+end]
		}
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// retireGentleKiro reports whether this plan retires Gentle AI's Kiro files.
func (p Plan) retireGentleKiro() bool {
	return p.Options.RetireGentleKiro && p.GentleKiro != nil && !p.GentleKiro.Empty()
}

// planGentleKiro inventories Gentle AI's files in the Kiro trees of the selected
// runtimes. It only plans a retirement when the user asked for it; otherwise it
// leaves the inventory and one note so the presenter can ask or doctor can warn.
func planGentleKiro(env Env, opts Options, pl *Plan) {
	roots := GentleKiroRoots(env, pl.RuntimeOrder)
	if len(roots) == 0 {
		return
	}
	inv := DetectGentleKiro(roots)
	if inv.Empty() {
		if opts.RetireGentleKiro {
			pl.note(NoteGentleKiroNone, "")
		}
		return
	}
	pl.GentleKiro = &inv
	if !opts.RetireGentleKiro {
		pl.note(NoteGentleKiroPresent, strconv.Itoa(inv.Counts().Files))
		return
	}
	for _, b := range inv.Bases {
		if b.Steering != nil {
			pl.add(rt.RemoveAfterVerify, b.Steering.Path, WhyGentleKiroSteering)
		}
		if len(b.Skills) > 0 {
			pl.add(rt.RemoveAfterVerify, filepath.Join(b.Root, "skills"), WhyGentleKiroSkills)
		}
		if len(b.Shared) > 0 {
			pl.add(rt.RemoveAfterVerify, filepath.Join(b.Root, "skills", "_shared"), WhyGentleKiroShared)
		}
		if len(b.Agents) > 0 {
			pl.add(rt.RemoveAfterVerify, filepath.Join(b.Root, "agents"), WhyGentleKiroAgents)
		}
	}
}

// backupGentleKiro copies every file to retire into <snapshot>/gentle-kiro/<n>/,
// keeping its path relative to the Kiro base, and verifies each copy by size and
// SHA-256. It returns the manifest entries. Any failure aborts before a single
// file is removed.
func backupGentleKiro(dir string, inv *GentleKiroInventory) ([]ManifestFile, error) {
	var out []ManifestFile
	for i, b := range inv.Bases {
		for _, f := range b.files() {
			data, err := os.ReadFile(f.Path)
			if errors.Is(err, fs.ErrNotExist) {
				continue // vanished since planning: nothing to save or remove
			}
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrGentleKiroBackup, err)
			}
			dst := filepath.Join(dir, gentleKiroDir, strconv.Itoa(i), f.Rel)
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrGentleKiroBackup, err)
			}
			if err := os.WriteFile(dst, data, 0o600); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrGentleKiroBackup, err)
			}
			if err := verifyCopy(dst, int64(len(data)), sha(data)); err != nil {
				return nil, fmt.Errorf("%w: %s: %v", ErrGentleKiroBackup, f.Rel, err)
			}
			rel, _ := filepath.Rel(dir, dst)
			out = append(out, ManifestFile{Path: f.Path, Runtime: GentleKiroRuntime, Existed: true, Copy: rel, SHA256: sha(data)})
		}
	}
	return out, nil
}

func verifyCopy(path string, size int64, hash string) error {
	got, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if int64(len(got)) != size || sha(got) != hash {
		return errors.New("the saved copy does not match the original")
	}
	return nil
}

// retireGentleKiro removes the files the snapshot already holds. It runs only
// after every critical check passed. First every saved copy is verified again;
// one bad copy aborts with nothing removed. A file whose content differs from
// the plan is skipped and reported, never removed. Only directories this step
// emptied are removed.
func (r *run) retireGentleKiro() error {
	planned := map[string]string{}
	for _, f := range r.pl.GentleKiro.Files() {
		planned[f.Path] = f.SHA256
	}
	var mine []ManifestFile
	for _, mf := range r.snap.Manifest.Files {
		if mf.Runtime == GentleKiroRuntime {
			mine = append(mine, mf)
		}
	}
	for _, mf := range mine {
		got, err := r.snap.copyOf(mf)
		if err != nil || sha(got) != mf.SHA256 {
			return fmt.Errorf("%w: the saved copy of %s is not intact", ErrGentleKiroBackup, mf.Path)
		}
	}
	res := &GentleKiroResult{Snapshot: r.snap.ID, Kept: r.pl.GentleKiro.KeptFiles()}
	r.res.GentleKiro = res
	for _, mf := range mine {
		cur, err := os.ReadFile(mf.Path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || sha(cur) != planned[mf.Path] || sha(cur) != mf.SHA256 {
			res.Skipped = append(res.Skipped, mf.Path)
			r.warn("not retired, it changed after the plan was made: %s", mf.Path)
			continue
		}
		if err := os.Remove(mf.Path); err != nil {
			res.Skipped = append(res.Skipped, mf.Path)
			r.warn("not retired: %v", err)
			continue
		}
		res.Removed = append(res.Removed, mf.Path)
	}
	for _, b := range r.pl.GentleKiro.Bases {
		pruneKiroDirs(b, res.Removed)
	}
	r.emit(PhaseRetire, "ok", "retire gentle-ai files in kiro", fmt.Sprintf("%d removed, %d kept in place", len(res.Removed), len(res.Skipped)))
	return nil
}

// pruneKiroDirs removes the directories that the removed files left empty, up to
// but never including skills/, agents/ and steering/. os.Remove refuses a
// directory that still holds anything.
func pruneKiroDirs(b GentleKiroBase, removed []string) {
	stop := map[string]bool{b.Root: true}
	for _, d := range []string{"skills", "agents", "steering"} {
		stop[filepath.Join(b.Root, d)] = true
	}
	for _, p := range removed {
		if rel, err := filepath.Rel(b.Root, p); err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		for dir := filepath.Dir(p); !stop[dir] && dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			if os.Remove(dir) != nil {
				break
			}
		}
	}
}
