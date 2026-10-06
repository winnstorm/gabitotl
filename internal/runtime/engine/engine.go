// Package engine holds the file-editing machinery shared by every runtime
// adapter: pure planners (Plan*) that read existing files and describe changes,
// and Apply/Remove that execute them through package ownership. Adapters only
// decide WHICH files, sections and keys to manage; this package decides HOW,
// so user content is never rewritten and every asset is recorded for removal.
package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/ownership"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// AssetVersion is recorded in the ownership state for assets written by this build.
const AssetVersion = "1"

func readFile(path string) ([]byte, bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return b, err == nil, err
}

func writeAtomic(path string, data []byte, created bool) error {
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gabitotl-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func sameJSON(a, b []byte) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}

func manual(runtime, path, target string, err error, snippet string) rt.Action {
	return rt.Action{Kind: rt.Preserve, Runtime: runtime, Path: path, Target: target,
		Detail: fmt.Sprintf("%v; add this by hand: %s", err, snippet), Edit: rt.Edit{Op: rt.OpManual}}
}

// PlanSection plans a Markdown section upsert. header is placed first when the file is created.
func PlanSection(runtime, path, name, body, header string) rt.Action {
	a := rt.Action{Runtime: runtime, Path: path, Target: "section " + name,
		Edit: rt.Edit{Op: rt.OpUpsertSection, Name: name, Body: body, Header: header}}
	data, exists, err := readFile(path)
	switch {
	case err != nil:
		a.Kind, a.Detail, a.Edit = rt.Preserve, err.Error(), rt.Edit{Op: rt.OpManual}
	case !exists:
		a.Kind = rt.Create
	default:
		cur, ok := ownership.SectionBody(string(data), name)
		switch {
		case ok && strings.TrimRight(cur, "\r\n") == strings.TrimRight(body, "\r\n"):
			a.Kind = rt.NoAction
		case ok:
			a.Kind, a.Detail = rt.Modify, "update managed section"
		default:
			a.Kind, a.Detail = rt.Modify, "append managed section (user content untouched)"
		}
	}
	return a
}

// PlanKey plans a managed JSON/JSONC key. A different value already present is
// kept (PRESERVE) unless overwrite says the user chose to replace it.
func PlanKey(runtime, path string, key []string, value json.RawMessage, overwrite bool) rt.Action {
	target := "key " + strings.Join(key, ".")
	a := rt.Action{Runtime: runtime, Path: path, Target: target, Edit: rt.Edit{Op: rt.OpSetKey, Key: key, Value: value}}
	data, exists, err := readFile(path)
	if err != nil {
		return manual(runtime, path, target, err, string(value))
	}
	if !exists {
		a.Kind = rt.Create
		return a
	}
	cur, ok, err := ownership.Get(data, key)
	if err != nil {
		return manual(runtime, path, target, err, fmt.Sprintf("%s = %s", strings.Join(key, "."), value))
	}
	switch {
	case !ok:
		a.Kind = rt.Modify
		if _, _, err := ownership.Set(data, key, value); err != nil { // dry run of the edit: refuse early
			return manual(runtime, path, target, err, fmt.Sprintf("%s = %s", strings.Join(key, "."), value))
		}
	case sameJSON(cur, value):
		a.Kind = rt.NoAction
	case overwrite:
		a.Kind, a.Detail = rt.Modify, "replace existing value (chosen by the user)"
	default:
		a.Kind, a.Detail = rt.Preserve, "a different value already exists and is kept"
		a.Edit = rt.Edit{Op: rt.OpManual}
	}
	return a
}

// PlanServer plans an MCP server entry inside container without duplicating
// one: if any existing member satisfies equivalent, nothing is written.
func PlanServer(runtime, path string, container []string, name string, value json.RawMessage, equivalent func(json.RawMessage) bool) rt.Action {
	if data, exists, err := readFile(path); err == nil && exists {
		if ms, err := ownership.Members(data, container); err == nil {
			for _, m := range ms {
				if equivalent(m.Raw) {
					return rt.Action{Kind: rt.NoAction, Runtime: runtime, Path: path, Target: "server " + m.Key,
						Detail: "an equivalent entry already exists; not duplicated", Edit: rt.Edit{Op: rt.OpSetKey, Key: append(append([]string(nil), container...), m.Key), Value: m.Raw}}
				}
			}
		}
	}
	return PlanKey(runtime, path, append(append([]string(nil), container...), name), value, false)
}

// PlanItem plans a managed string item in a JSON array.
func PlanItem(runtime, path string, key []string, item string) rt.Action {
	target := fmt.Sprintf("item %s[%s]", strings.Join(key, "."), item)
	a := rt.Action{Runtime: runtime, Path: path, Target: target, Edit: rt.Edit{Op: rt.OpAddItem, Key: key, Item: item}}
	data, exists, err := readFile(path)
	if err != nil {
		return manual(runtime, path, target, err, item)
	}
	if !exists {
		a.Kind = rt.Create
		return a
	}
	has, err := ownership.ArrayHas(data, key, item)
	if err == nil && !has {
		_, _, _, err = ownership.ArrayAdd(data, key, item)
	}
	switch {
	case err != nil:
		return manual(runtime, path, target, err, fmt.Sprintf("add %q to %s", item, strings.Join(key, ".")))
	case has:
		a.Kind = rt.NoAction
	default:
		a.Kind = rt.Modify
	}
	return a
}

// Foreign marker families the migration may retire (spec section 37). The
// Gentle-AI marker format is an ASSUMPTION (see the claudecode package doc).
var foreign = map[string]struct {
	start *regexp.Regexp
	end   string
}{
	"gentle-ai": {regexp.MustCompile(`<!--\s*gentle-ai:([A-Za-z0-9._-]+)\s*-->`), `<!--\s*/gentle-ai:%s\s*-->`},
}

// ForeignPersonaBlocks returns the managed persona blocks of another harness.
func ForeignPersonaBlocks(content, family string) []ownership.Block {
	f, ok := foreign[family]
	if !ok {
		return nil
	}
	var out []ownership.Block
	for _, b := range ownership.FindBlocks(content, f.start, f.end) {
		if n := strings.ToLower(b.Name); strings.Contains(n, "persona") || strings.Contains(n, "gentleman") {
			out = append(out, b)
		}
	}
	return out
}

// PlanForeignPersona reports a persona conflict in path. The managed block of
// the other harness is removed only after verification and only when migrate is
// set; otherwise the conflict is surfaced as a PRESERVE action.
func PlanForeignPersona(runtime, path, family string, migrate bool) []rt.Action {
	data, exists, err := readFile(path)
	if err != nil || !exists {
		return nil
	}
	var out []rt.Action
	for _, b := range ForeignPersonaBlocks(string(data), family) {
		a := rt.Action{Runtime: runtime, Path: path, Target: family + " block " + b.Name,
			Edit: rt.Edit{Op: rt.OpRemoveBlock, Name: b.Name, Foreign: family}}
		if migrate {
			a.Kind, a.Detail = rt.RemoveAfterVerify, "persona conflict: remove only the managed block once GabitoTL is verified"
		} else {
			a.Kind, a.Detail, a.Edit = rt.Preserve, "persona conflict: two personas would apply; rerun with --migrate-persona to retire this managed block", rt.Edit{Op: rt.OpManual}
		}
		out = append(out, a)
	}
	return out
}

// Inspect builds the read-only view of paths.
func Inspect(env rt.Env, runtime string, paths ...string) rt.Inspection {
	in := rt.Inspection{Runtime: runtime}
	st, _ := env.Store().Load()
	for _, p := range paths {
		data, exists, _ := readFile(p)
		fi := rt.FileInfo{Path: p, Exists: exists}
		if i, ok := st.Find(p); ok {
			fi.Managed = containsStr(st.Entries[i].Runtimes, runtime)
		}
		in.Files = append(in.Files, fi)
		if exists {
			for family := range foreign {
				for _, b := range ForeignPersonaBlocks(string(data), family) {
					in.Conflicts = append(in.Conflicts, rt.Conflict{Kind: "persona", Path: p, Detail: family + " persona block " + b.Name})
				}
			}
		}
	}
	return in
}

func containsStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// Backup copies the existing files plan touches into snap (global backup, spec 40).
func Backup(env rt.Env, snap *rt.Snapshot, plan []rt.Action) error {
	if snap.Dir == "" {
		return errors.New("engine: snapshot directory is not set")
	}
	seen := map[string]bool{}
	for _, a := range plan {
		if a.Edit.Op == rt.OpManual || a.Kind == rt.NoAction || seen[a.Path] {
			continue
		}
		seen[a.Path] = true
		data, exists, err := readFile(a.Path)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		rel := strings.NewReplacer(":", "_", `\`, "/").Replace(a.Path)
		dst := filepath.Join(snap.Dir, "files", strings.TrimLeft(rel, "/"))
		if err := writeAtomic(dst, data, true); err != nil {
			return err
		}
		snap.Files = append(snap.Files, a.Path)
	}
	return nil
}

// check verifies one Create/Modify/NoAction action against the file on disk.
func check(a rt.Action) rt.Check {
	c := rt.Check{Name: a.Path + " [" + a.Target + "]"}
	data, _, _ := readFile(a.Path)
	switch a.Edit.Op {
	case rt.OpUpsertSection:
		body, ok := ownership.SectionBody(string(data), a.Edit.Name)
		c.OK = ok && strings.TrimRight(body, "\r\n") == strings.TrimRight(a.Edit.Body, "\r\n")
	case rt.OpSetKey:
		cur, ok, _ := ownership.Get(data, a.Edit.Key)
		c.OK = ok && sameJSON(cur, a.Edit.Value)
	case rt.OpAddItem:
		c.OK, _ = ownership.ArrayHas(data, a.Edit.Key, a.Edit.Item)
	default:
		c.OK = true
	}
	if !c.OK {
		c.Detail = "not in the expected state"
	}
	return c
}

// Pending turns a fresh plan into verification checks: anything still to be
// created or modified is a failed check.
func Pending(plan []rt.Action) []rt.Check {
	var out []rt.Check
	for _, a := range plan {
		switch a.Kind {
		case rt.Create, rt.Modify:
			out = append(out, rt.Check{Name: a.Path + " [" + a.Target + "]", OK: false, Detail: string(a.Kind) + " still pending"})
		case rt.NoAction:
			out = append(out, check(a))
		default:
			if a.Edit.Op == rt.OpManual {
				out = append(out, rt.Check{Name: a.Path + " [" + a.Target + "]", OK: true, Detail: "kept; " + a.Detail})
			}
		}
	}
	return out
}

// Apply executes plan for one adapter. CREATE/MODIFY run first, are verified,
// and only then do REMOVE-AFTER-VERIFY actions run. Every managed asset is
// recorded in the global ownership state.
func Apply(env rt.Env, runtime, version string, plan []rt.Action) (rt.Result, error) {
	var res rt.Result
	if version == "" {
		version = AssetVersion
	}
	store := env.Store()
	st, err := store.Load()
	if err != nil {
		return res, err
	}
	for _, a := range plan {
		switch a.Kind {
		case rt.Create, rt.Modify:
			if err := applyOne(&st, runtime, version, a); err != nil {
				var ue *ownership.UnsafeError
				if errors.As(err, &ue) {
					res.Manual = append(res.Manual, manual(runtime, a.Path, a.Target, err, "see plan"))
					continue
				}
				return res, fmt.Errorf("%s: %w", a.Path, err)
			}
			res.Applied = append(res.Applied, a)
		case rt.NoAction:
			adopt(&st, runtime, version, a)
		case rt.Preserve:
			if a.Edit.Op == rt.OpManual {
				res.Manual = append(res.Manual, a)
			}
		}
	}
	if err := store.Save(st); err != nil {
		return res, err
	}
	failed := false
	for _, a := range res.Applied {
		c := check(a)
		res.Checks = append(res.Checks, c)
		failed = failed || !c.OK
	}
	if failed {
		return res, errors.New("verification failed; REMOVE-AFTER-VERIFY actions were not run")
	}
	for _, a := range plan {
		if a.Kind != rt.RemoveAfterVerify {
			continue
		}
		if err := removeForeign(a); err != nil {
			return res, fmt.Errorf("%s: %w", a.Path, err)
		}
		res.Removed = append(res.Removed, a)
	}
	return res, nil
}

func removeForeign(a rt.Action) error {
	data, exists, err := readFile(a.Path)
	if err != nil || !exists {
		return err
	}
	content := string(data)
	for _, b := range ForeignPersonaBlocks(content, a.Edit.Foreign) {
		if b.Name == a.Edit.Name {
			return writeAtomic(a.Path, []byte(ownership.RemoveBlock(content, b)), false)
		}
	}
	return nil
}

// adopt records a pre-existing identical asset that we manage jointly (for
// example the shared Kiro files) without rewriting the file.
func adopt(st *ownership.State, runtime, version string, a rt.Action) {
	i, ok := st.Find(a.Path)
	if !ok {
		return
	}
	e := st.Entries[i]
	switch a.Edit.Op {
	case rt.OpUpsertSection:
		if !containsStr(e.Sections, a.Edit.Name) {
			return
		}
	case rt.OpSetKey, rt.OpAddItem:
		found := false
		for _, k := range e.Keys {
			found = found || (equalPath(k.Path, a.Edit.Key) && k.Item == a.Edit.Item)
		}
		if !found {
			return
		}
	default:
		return
	}
	st.Upsert(ownership.Entry{Runtimes: []string{runtime}, Path: a.Path, HashAfter: e.HashAfter, Version: version})
}

func equalPath(a, b []string) bool { return strings.Join(a, "\x00") == strings.Join(b, "\x00") }

func applyOne(st *ownership.State, runtime, version string, a rt.Action) error {
	data, exists, err := readFile(a.Path)
	if err != nil {
		return err
	}
	entry := ownership.Entry{Runtimes: []string{runtime}, Path: a.Path, CreatedFile: !exists, HashBefore: ownership.Hash(data), Version: version}
	var out []byte
	switch a.Edit.Op {
	case rt.OpUpsertSection:
		content := string(data)
		if !exists {
			content = a.Edit.Header
		}
		s, err := ownership.UpsertSection(content, a.Edit.Name, a.Edit.Body)
		if err != nil {
			return err
		}
		out, entry.Sections = []byte(s), []string{a.Edit.Name}
	case rt.OpSetKey:
		if cur, ok, _ := ownership.Get(data, a.Edit.Key); ok && sameJSON(cur, a.Edit.Value) {
			out = data // identical entry already there: adopt, do not rewrite
			entry.Keys = nil
			if i, found := st.Find(a.Path); found && hasKey(st.Entries[i], a.Edit.Key, "") {
				entry.Keys = []ownership.Key{{Path: a.Edit.Key}}
			}
			break
		}
		var r ownership.SetResult
		if out, r, err = ownership.Set(data, a.Edit.Key, a.Edit.Value); err != nil {
			return err
		}
		entry.Keys = []ownership.Key{{Path: a.Edit.Key, Previous: r.Previous, Prune: r.Created}}
	case rt.OpAddItem:
		var added, created bool
		if out, added, created, err = ownership.ArrayAdd(data, a.Edit.Key, a.Edit.Item); err != nil {
			return err
		}
		if added {
			entry.Keys = []ownership.Key{{Path: a.Edit.Key, Item: a.Edit.Item, Prune: created}}
		}
	default:
		return fmt.Errorf("engine: cannot apply op %q", a.Edit.Op)
	}
	if !bytes.Equal(out, data) || !exists {
		if err := writeAtomic(a.Path, out, !exists); err != nil {
			return err
		}
	}
	entry.HashAfter = ownership.Hash(out)
	if _, ok := st.Find(a.Path); ok {
		entry.HashBefore = ""
	}
	st.Upsert(entry)
	return nil
}

func hasKey(e ownership.Entry, path []string, item string) bool {
	for _, k := range e.Keys {
		if equalPath(k.Path, path) && k.Item == item {
			return true
		}
	}
	return false
}

// Remove undoes exactly what the ownership state says runtime wrote. Shared
// files are only cleaned once their last owner is removed. Content outside our
// markers and keys is never touched.
func Remove(env rt.Env, runtime string) (rt.Result, error) {
	var res rt.Result
	store := env.Store()
	st, err := store.Load()
	if err != nil {
		return res, err
	}
	var keep []ownership.Entry
	for _, e := range st.Entries {
		if !containsStr(e.Runtimes, runtime) {
			keep = append(keep, e)
			continue
		}
		if len(e.Runtimes) > 1 { // shared: another runtime still depends on it
			e.Runtimes = without(e.Runtimes, runtime)
			keep = append(keep, e)
			continue
		}
		if err := removeEntry(e); err != nil {
			var ue *ownership.UnsafeError
			if errors.As(err, &ue) {
				res.Manual = append(res.Manual, manual(runtime, e.Path, "managed assets", err, "remove the GabitoTL keys listed in ~/.gabitotl/state.json"))
				keep = append(keep, e)
				continue
			}
			return res, fmt.Errorf("%s: %w", e.Path, err)
		}
		res.Removed = append(res.Removed, rt.Action{Kind: rt.RemoveAfterVerify, Runtime: runtime, Path: e.Path, Target: "managed assets"})
	}
	st.Entries = keep
	return res, store.Save(st)
}

func without(l []string, s string) []string {
	var out []string
	for _, x := range l {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

func removeEntry(e ownership.Entry) error {
	data, exists, err := readFile(e.Path)
	if err != nil || !exists {
		return err
	}
	out := data
	for _, name := range e.Sections {
		s, _ := ownership.RemoveSection(string(out), name)
		out = []byte(s)
	}
	for _, k := range e.Keys {
		switch {
		case k.Item != "":
			out, _, err = ownership.ArrayRemove(out, k.Path, k.Item, k.Prune)
		case len(k.Previous) > 0:
			out, _, err = ownership.Set(out, k.Path, k.Previous)
		default:
			out, _, err = ownership.Remove(out, k.Path, k.Prune)
		}
		if err != nil {
			return err
		}
	}
	if e.CreatedFile && isEmpty(out) {
		return os.Remove(e.Path)
	}
	if bytes.Equal(out, data) {
		return nil
	}
	return writeAtomic(e.Path, out, false)
}

// isEmpty reports whether nothing but whitespace, an empty JSON object or bare
// front matter is left, meaning the file only ever held our assets.
func isEmpty(b []byte) bool {
	t := bytes.TrimSpace(b)
	if len(t) == 0 || string(t) == "{}" {
		return true
	}
	if d, err := gabitotl.ParseDoc(b); err == nil && d.HasFront && strings.TrimSpace(d.Body) == "" {
		return true
	}
	return false
}
