package ownership

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrUnsafe is matched (errors.Is) by every *UnsafeError: the file cannot be
// edited without risking the user's content, so callers must propose a manual
// action instead of rewriting it.
var ErrUnsafe = errors.New("ownership: file cannot be edited safely")

// UnsafeError explains why a JSON/JSONC document was refused.
type UnsafeError struct{ Reason string }

func (e *UnsafeError) Error() string { return "cannot edit safely: " + e.Reason }
func (e *UnsafeError) Unwrap() error { return ErrUnsafe }

func unsafef(format string, a ...any) error { return &UnsafeError{fmt.Sprintf(format, a...)} }

// The editor below never re-serializes a document. It scans JSON/JSONC (line
// and block comments, trailing commas) to find byte ranges and splices only the
// managed text, so comments, ordering, indentation and every other key survive
// byte for byte. A document it cannot scan is refused with *UnsafeError.

type member struct {
	key                        string // empty for array elements
	keyStart, valStart, valEnd int
}

type scanner struct{ d []byte }

func (s scanner) skip(i int) (int, error) {
	for i < len(s.d) {
		switch c := s.d[i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '/' && i+1 < len(s.d) && s.d[i+1] == '/':
			for i < len(s.d) && s.d[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(s.d) && s.d[i+1] == '*':
			j := bytes.Index(s.d[i+2:], []byte("*/"))
			if j < 0 {
				return i, unsafef("unterminated block comment")
			}
			i += j + 4
		default:
			return i, nil
		}
	}
	return i, nil
}

func (s scanner) str(i int) (int, error) {
	for j := i + 1; j < len(s.d); j++ {
		switch s.d[j] {
		case '\\':
			j++
		case '"':
			return j + 1, nil
		case '\n':
			return i, unsafef("newline in string")
		}
	}
	return i, unsafef("unterminated string")
}

func (s scanner) value(i int) (int, error) {
	if i >= len(s.d) {
		return i, unsafef("unexpected end of document")
	}
	switch s.d[i] {
	case '"':
		return s.str(i)
	case '{', '[':
		_, closeIdx, err := s.container(i)
		return closeIdx + 1, err
	}
	j := i
	for j < len(s.d) && !strings.ContainsRune(",}] \t\r\n/", rune(s.d[j])) {
		j++
	}
	lit := string(s.d[i:j])
	var v any
	if lit == "" || json.Unmarshal([]byte(lit), &v) != nil {
		return i, unsafef("unexpected token %q", lit)
	}
	return j, nil
}

// container scans the object or array opening at i and returns its members
// (elements for arrays) and the index of the closing bracket.
func (s scanner) container(i int) ([]member, int, error) {
	if i >= len(s.d) || (s.d[i] != '{' && s.d[i] != '[') {
		return nil, i, unsafef("expected an object or array")
	}
	isObj := s.d[i] == '{'
	closer := byte(']')
	if isObj {
		closer = '}'
	}
	var ms []member
	i++
	for {
		var err error
		if i, err = s.skip(i); err != nil {
			return nil, i, err
		}
		if i >= len(s.d) {
			return nil, i, unsafef("unterminated container")
		}
		if s.d[i] == closer {
			return ms, i, nil
		}
		m := member{keyStart: i}
		if isObj {
			if s.d[i] != '"' {
				return nil, i, unsafef("expected a quoted key")
			}
			end, err := s.str(i)
			if err != nil {
				return nil, i, err
			}
			if err := json.Unmarshal(s.d[i:end], &m.key); err != nil {
				return nil, i, unsafef("invalid key")
			}
			if i, err = s.skip(end); err != nil {
				return nil, i, err
			}
			if i >= len(s.d) || s.d[i] != ':' {
				return nil, i, unsafef("expected ':'")
			}
			if i, err = s.skip(i + 1); err != nil {
				return nil, i, err
			}
		}
		m.valStart = i
		if m.valEnd, err = s.value(i); err != nil {
			return nil, i, err
		}
		ms = append(ms, m)
		if i, err = s.skip(m.valEnd); err != nil {
			return nil, i, err
		}
		if i < len(s.d) && s.d[i] == ',' {
			i++
		} else if i >= len(s.d) || s.d[i] != closer {
			return nil, i, unsafef("expected ',' or closing bracket")
		}
	}
}

// root returns the index of the root object, or -1 for an empty document.
func (s scanner) root() (int, error) {
	i, err := s.skip(0)
	if err != nil {
		return 0, err
	}
	if i >= len(s.d) {
		return -1, nil
	}
	if s.d[i] != '{' {
		return 0, unsafef("the root is not an object")
	}
	end, err := s.value(i)
	if err != nil {
		return 0, err
	}
	if tail, err := s.skip(end); err != nil || tail != len(s.d) {
		return 0, unsafef("unexpected content after the root object")
	}
	return i, nil
}

// find walks path from the root and returns the opening index of the container
// at path (the root for an empty path).
func (s scanner) find(path []string) (open int, found bool, err error) {
	open, err = s.root()
	if err != nil || open < 0 {
		return 0, false, err
	}
	for _, k := range path {
		ms, _, err := s.container(open)
		if err != nil {
			return 0, false, err
		}
		idx := indexKey(ms, k)
		if idx < 0 {
			return 0, false, nil
		}
		open = ms[idx].valStart
		if open >= len(s.d) || (s.d[open] != '{' && s.d[open] != '[') {
			return 0, false, unsafef("%q is not an object or array", strings.Join(path, "."))
		}
	}
	return open, true, nil
}

func indexKey(ms []member, k string) int {
	for i, m := range ms {
		if m.key == k {
			return i
		}
	}
	return -1
}

func (s scanner) lineIndent(pos int) (indent string, onlyWS bool) {
	start := bytes.LastIndexByte(s.d[:pos], '\n') + 1
	seg := string(s.d[start:pos])
	return seg, strings.TrimLeft(seg, " \t") == ""
}

func splice(d []byte, from, to int, repl string) []byte {
	out := make([]byte, 0, len(d)-(to-from)+len(repl))
	out = append(out, d[:from]...)
	out = append(out, repl...)
	return append(out, d[to:]...)
}

func compact(raw json.RawMessage) (string, error) {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return "", fmt.Errorf("ownership: invalid JSON value: %w", err)
	}
	return b.String(), nil
}

func indentValue(compactRaw, prefix string) string {
	var b bytes.Buffer
	if json.Indent(&b, []byte(compactRaw), prefix, "  ") != nil {
		return compactRaw
	}
	return b.String()
}

// insert adds text for a new member (key empty for arrays) to the container at open.
func (s scanner) insert(open int, key, rawValue string) ([]byte, error) {
	ms, closeIdx, err := s.container(open)
	if err != nil {
		return nil, err
	}
	closeIndent, _ := s.lineIndent(closeIdx)
	multiline := bytes.IndexByte(s.d[open:closeIdx], '\n') >= 0
	memIndent := closeIndent + "  "
	if len(ms) > 0 {
		if ind, ok := s.lineIndent(ms[len(ms)-1].keyStart); ok {
			memIndent = ind
		}
	}
	entry := func(prefix string) string {
		v := indentValue(rawValue, prefix)
		if key == "" {
			return v
		}
		k, _ := json.Marshal(key)
		return string(k) + ": " + v
	}
	switch {
	case !multiline && len(ms) == 0:
		return splice(s.d, open+1, closeIdx, entry("")), nil
	case !multiline:
		return splice(s.d, ms[len(ms)-1].valEnd, ms[len(ms)-1].valEnd, ", "+entry("")), nil
	case len(ms) == 0:
		return splice(s.d, open+1, open+1, "\n"+memIndent+entry(memIndent)), nil
	}
	last := ms[len(ms)-1].valEnd
	return splice(s.d, last, last, ",\n"+memIndent+entry(memIndent)), nil
}

// removeMember deletes member idx of the container at open, with its comma and line.
func (s scanner) removeMember(ms []member, idx int) []byte {
	m := ms[idx]
	start, end := m.keyStart, m.valEnd
	_, lineStart := s.lineIndent(start)
	if lineStart {
		start = bytes.LastIndexByte(s.d[:start], '\n') + 1
	}
	j, _ := s.skip(end)
	hasComma := j < len(s.d) && s.d[j] == ','
	if hasComma {
		end = j + 1
	}
	if lineStart { // swallow the rest of the line when only whitespace remains
		k := end
		for k < len(s.d) && (s.d[k] == ' ' || s.d[k] == '\t' || s.d[k] == '\r') {
			k++
		}
		if k < len(s.d) && s.d[k] == '\n' {
			end = k + 1
		}
	} else if hasComma {
		for end < len(s.d) && s.d[end] == ' ' {
			end++
		}
	}
	if !hasComma && idx > 0 && !lineStart { // single-line object: drop ", member" whole
		if c, _ := s.skip(ms[idx-1].valEnd); c < start && s.d[c] == ',' {
			return splice(s.d, c, end, "")
		}
	}
	out := splice(s.d, start, end, "")
	if !hasComma && idx > 0 { // the last member left a dangling comma behind
		p := ms[idx-1].valEnd
		if c, _ := s.skip(p); c < start && s.d[c] == ',' {
			out = splice(out, c, c+1, "")
		}
	}
	return out
}

func decodeString(raw []byte) (string, bool) {
	var v string
	return v, json.Unmarshal(raw, &v) == nil
}

// SetResult describes what Set changed.
type SetResult struct {
	Created  bool            // the first path element did not exist before (a parent may need pruning on removal)
	Previous json.RawMessage // old value when the key already existed
}

// Set writes value at the object path, creating missing objects. Other bytes
// of data are untouched. An empty data is treated as an empty object.
func Set(data []byte, path []string, value json.RawMessage) ([]byte, SetResult, error) {
	if len(path) == 0 {
		return nil, SetResult{}, errors.New("ownership: empty key path")
	}
	raw, err := compact(value)
	if err != nil {
		return nil, SetResult{}, err
	}
	if len(bytes.TrimSpace(data)) == 0 && bytes.IndexByte(data, '/') < 0 {
		data = []byte("{}\n")
	}
	s := scanner{data}
	cur, err := s.root()
	if err != nil {
		return nil, SetResult{}, err
	}
	res := SetResult{}
	for i, k := range path {
		ms, _, err := s.container(cur)
		if err != nil {
			return nil, res, err
		}
		idx := indexKey(ms, k)
		last := i == len(path)-1
		if idx < 0 {
			res.Created = i == 0
			nested := raw
			for j := len(path) - 1; j > i; j-- {
				kb, _ := json.Marshal(path[j])
				nested = "{" + string(kb) + ":" + nested + "}"
			}
			out, err := s.insert(cur, k, nested)
			return out, res, err
		}
		if last {
			res.Previous = json.RawMessage(append([]byte(nil), data[ms[idx].valStart:ms[idx].valEnd]...))
			return splice(data, ms[idx].valStart, ms[idx].valEnd, indentValue(raw, lineIndentOf(s, ms[idx].keyStart))), res, nil
		}
		cur = ms[idx].valStart
		if data[cur] != '{' {
			return nil, res, unsafef("%q is not an object", k)
		}
	}
	return data, res, nil
}

func lineIndentOf(s scanner, pos int) string {
	if ind, ok := s.lineIndent(pos); ok {
		return ind
	}
	return ""
}

// Get returns the raw value at path.
func Get(data []byte, path []string) (json.RawMessage, bool, error) {
	s := scanner{data}
	if len(path) == 0 {
		return nil, false, errors.New("ownership: empty key path")
	}
	open, found, err := s.find(path[:len(path)-1])
	if err != nil || !found {
		return nil, false, err
	}
	ms, _, err := s.container(open)
	if err != nil {
		return nil, false, err
	}
	if idx := indexKey(ms, path[len(path)-1]); idx >= 0 {
		return json.RawMessage(data[ms[idx].valStart:ms[idx].valEnd]), true, nil
	}
	return nil, false, nil
}

// Member is one key and raw value of an object.
type Member struct {
	Key string
	Raw json.RawMessage
}

// Members lists the members of the object at path (empty path: the root).
func Members(data []byte, path []string) ([]Member, error) {
	s := scanner{data}
	open, found, err := s.find(path)
	if err != nil || !found {
		return nil, err
	}
	ms, _, err := s.container(open)
	if err != nil {
		return nil, err
	}
	out := make([]Member, len(ms))
	for i, m := range ms {
		out[i] = Member{m.key, json.RawMessage(data[m.valStart:m.valEnd])}
	}
	return out, nil
}

// Remove deletes the key at path. With prune it also deletes parents left
// empty. It reports whether anything was removed.
func Remove(data []byte, path []string, prune bool) ([]byte, bool, error) {
	if len(path) == 0 {
		return nil, false, errors.New("ownership: empty key path")
	}
	s := scanner{data}
	open, found, err := s.find(path[:len(path)-1])
	if err != nil || !found {
		return data, false, err
	}
	ms, _, err := s.container(open)
	if err != nil {
		return nil, false, err
	}
	idx := indexKey(ms, path[len(path)-1])
	if idx < 0 {
		return data, false, nil
	}
	out := s.removeMember(ms, idx)
	if prune && len(path) > 1 {
		if rest, err := Members(out, path[:len(path)-1]); err == nil && len(rest) == 0 {
			if pruned, _, err := Remove(out, path[:len(path)-1], true); err == nil {
				out = pruned
			}
		}
	}
	return out, true, nil
}

// ArrayAdd appends the string item to the array at path unless present,
// creating the array when missing. created reports a brand-new array.
func ArrayAdd(data []byte, path []string, item string) (out []byte, added, created bool, err error) {
	has, err := ArrayHas(data, path, item)
	if err != nil || has {
		return data, false, false, err
	}
	s := scanner{data}
	open, found, err := s.find(path)
	if err != nil {
		return nil, false, false, err
	}
	b, _ := json.Marshal(item)
	if !found {
		out, res, err := Set(data, path, json.RawMessage("["+string(b)+"]"))
		return out, err == nil, res.Created, err
	}
	if data[open] != '[' {
		return nil, false, false, unsafef("%q is not an array", strings.Join(path, "."))
	}
	out, err = s.insert(open, "", string(b))
	return out, err == nil, false, err
}

// ArrayHas reports whether the array at path contains the string item.
func ArrayHas(data []byte, path []string, item string) (bool, error) {
	s := scanner{data}
	open, found, err := s.find(path)
	if err != nil || !found {
		return false, err
	}
	if data[open] != '[' {
		return false, unsafef("%q is not an array", strings.Join(path, "."))
	}
	ms, _, err := s.container(open)
	if err != nil {
		return false, err
	}
	for _, m := range ms {
		if v, ok := decodeString(data[m.valStart:m.valEnd]); ok && v == item {
			return true, nil
		}
	}
	return false, nil
}

// ArrayRemove deletes the string item from the array at path. With prune an
// emptied array (and parents left empty) is deleted too.
func ArrayRemove(data []byte, path []string, item string, prune bool) ([]byte, bool, error) {
	s := scanner{data}
	open, found, err := s.find(path)
	if err != nil || !found {
		return data, false, err
	}
	ms, _, err := s.container(open)
	if err != nil {
		return nil, false, err
	}
	for i, m := range ms {
		if v, ok := decodeString(data[m.valStart:m.valEnd]); ok && v == item {
			out := s.removeMember(ms, i)
			if prune && len(ms) == 1 {
				if pruned, _, err := Remove(out, path, true); err == nil {
					out = pruned
				}
			}
			return out, true, nil
		}
	}
	return data, false, nil
}
