package desktop

import (
	"os"
	"strings"
)

// lookupEnv resolves %NAME% references; tests replace it.
var lookupEnv = os.LookupEnv

// ExpandPercent replaces %NAME% with lookup(NAME), like cmd and the registry's
// REG_EXPAND_SZ do. Unknown names and stray % signs are left untouched.
func ExpandPercent(s string, lookup func(string) (string, bool)) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '%')
		if i < 0 {
			break
		}
		j := strings.IndexByte(s[i+1:], '%')
		if j < 0 {
			break
		}
		name := s[i+1 : i+1+j]
		if v, ok := lookup(name); ok && name != "" {
			b.WriteString(s[:i])
			b.WriteString(v)
			s = s[i+j+2:]
			continue
		}
		b.WriteString(s[:i+1])
		s = s[i+1:]
	}
	b.WriteString(s)
	return b.String()
}

// normalizeEntry makes two spellings of the same directory compare equal:
// surrounding quotes and whitespace, trailing separators and case are ignored
// (Windows paths are case-insensitive).
func normalizeEntry(e string) string {
	e = strings.TrimSpace(e)
	e = strings.Trim(e, `"`)
	e = strings.TrimRight(e, `\/`)
	return strings.ToLower(e)
}

// HasPathEntry reports whether the semicolon-separated PATH value contains
// dir, also counting entries that only equal it once %VARS% are expanded.
func HasPathEntry(path, dir string) bool {
	want := normalizeEntry(dir)
	if want == "" {
		return false
	}
	for _, e := range strings.Split(path, ";") {
		if normalizeEntry(e) == want || normalizeEntry(ExpandPercent(e, lookupEnv)) == want {
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

// RemovePathEntry deletes every literal spelling of dir (entries that only
// match through %VARS% are left alone). It reports whether the value
// changed; when it did, empty entries are dropped.
func RemovePathEntry(path, dir string) (string, bool) {
	want := normalizeEntry(dir)
	var keep []string
	found := false
	for _, e := range nonEmpty(path) {
		if want != "" && normalizeEntry(e) == want {
			found = true
			continue
		}
		keep = append(keep, e)
	}
	if !found {
		return path, false
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
