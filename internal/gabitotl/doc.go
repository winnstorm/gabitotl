package gabitotl

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Doc is a Markdown document with optional YAML front matter.
// Body is preserved byte for byte.
type Doc struct {
	Front    []byte // raw YAML between the --- fences (no fences)
	HasFront bool
	Body     string
}

const fence = "---"

// ParseDoc splits data into front matter and Markdown body.
// A document without a leading fence is all body.
func ParseDoc(data []byte) (Doc, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	s := string(data)
	first, rest, ok := cutLine(s)
	if !ok || strings.TrimRight(first, " \t\r\n") != fence {
		return Doc{Body: s}, nil
	}
	var front strings.Builder
	for rest != "" {
		var line string
		line, rest, _ = cutLine(rest)
		if t := strings.TrimRight(line, " \t\r\n"); t == fence || t == "..." {
			return Doc{Front: []byte(front.String()), HasFront: true, Body: rest}, nil
		}
		front.WriteString(line)
	}
	return Doc{}, errors.New("front matter is not terminated by '---'")
}

// cutLine returns the first line including its newline, and the remainder.
func cutLine(s string) (line, rest string, ok bool) {
	if s == "" {
		return "", "", false
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i+1], s[i+1:], true
	}
	return s, "", true
}

// Decode unmarshals the front matter into v.
func (d Doc) Decode(v any) error {
	if !d.HasFront {
		return errors.New("document has no front matter")
	}
	if err := yaml.Unmarshal(d.Front, v); err != nil {
		return fmt.Errorf("invalid front matter: %w", err)
	}
	return nil
}

// Bytes renders the document back to Markdown.
func (d Doc) Bytes() []byte {
	if !d.HasFront {
		return []byte(d.Body)
	}
	front := string(d.Front)
	if front != "" && !strings.HasSuffix(front, "\n") {
		front += "\n"
	}
	return []byte(fence + "\n" + front + fence + "\n" + d.Body)
}

// RenderDoc marshals front (a struct or map) as YAML front matter followed by body.
func RenderDoc(front any, body string) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(front); err != nil {
		return nil, fmt.Errorf("render front matter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return Doc{Front: buf.Bytes(), HasFront: true, Body: body}.Bytes(), nil
}
