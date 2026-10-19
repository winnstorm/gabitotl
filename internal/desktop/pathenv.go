package desktop

import (
	"strings"
)

// normalizeEntry makes two spellings of the same directory compare equal:
// surrounding quotes and whitespace, trailing separators and case are ignored
// (Windows paths are case-insensitive).
func normalizeEntry(e string) string {
	e = strings.TrimSpace(e)
	e = strings.Trim(e, `"`)
	e = strings.TrimRight(e, `\/`)
	return strings.ToLower(e)
}

// HasPathEntry reports whether the semicolon-separated PATH value contains dir.
func HasPathEntry(path, dir string) bool {
	want := normalizeEntry(dir)
	if want == "" {
		return false
	}
	for _, e := range strings.Split(path, ";") {
		if normalizeEntry(e) == want {
			return true
		}
	}
	return false
}

// AddPathEntry appends dir unless it is already present. It reports whether
// the value changed; when it did, empty entries are dropped.
func AddPathEntry(path, dir string) (string, bool) {
	if strings.TrimSpace(dir) == "" || HasPathEntry(path, dir) {
		return path, false
	}
	return strings.Join(append(nonEmpty(path), dir), ";"), true
}

// RemovePathEntry deletes every spelling of dir. It reports whether the value
// changed; when it did, empty entries are dropped.
func RemovePathEntry(path, dir string) (string, bool) {
	if !HasPathEntry(path, dir) {
		return path, false
	}
	want := normalizeEntry(dir)
	var keep []string
	for _, e := range nonEmpty(path) {
		if normalizeEntry(e) != want {
			keep = append(keep, e)
		}
	}
	return strings.Join(keep, ";"), true
}

func nonEmpty(path string) []string {
	var out []string
	for _, e := range strings.Split(path, ";") {
		if strings.TrimSpace(e) != "" {
			out = append(out, e)
		}
	}
	return out
}
