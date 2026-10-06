package persona

import (
	"bufio"
	_ "embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed messages_es-AR.txt
var defaultMessages string

// Message keys.
const (
	KeyBlockDid      = "block.did"
	KeyBlockProgress = "block.progress"
	KeyBlockNext     = "block.next"
	KeyBlockOjo      = "block.ojo"
	KeyBlockTip      = "block.tip"
	KeySmallDone     = "small.done"
	KeyOjoOverlap    = "ojo.overlap"
	KeyTipLongChat   = "tip.long_chat"
)

var requiredKeys = []string{
	KeyBlockDid, KeyBlockProgress, KeyBlockNext, KeyBlockOjo, KeyBlockTip,
	KeySmallDone, KeyOjoOverlap, KeyTipLongChat,
}

// Messages holds the replaceable user-facing templates.
type Messages map[string]string

// ParseMessages reads "key = value" lines (blank lines and # comments ignored)
// and verifies every required key is present.
func ParseMessages(src string) (Messages, error) {
	m := Messages{}
	sc := bufio.NewScanner(strings.NewReader(src))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("messages: line %d: expected key = value", n)
		}
		m[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	var missing []string
	for _, k := range requiredKeys {
		if m[k] == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("messages: missing keys: %s", strings.Join(missing, ", "))
	}
	return m, nil
}

// DefaultMessages returns the embedded es-AR templates.
func DefaultMessages() Messages {
	m, err := ParseMessages(defaultMessages)
	if err != nil {
		panic(err) // embedded file is covered by tests
	}
	return m
}

// Format returns the template for key with {name} placeholders replaced by the
// given name/value pairs.
func (m Messages) Format(key string, pairs ...string) string {
	s := m[key]
	for i := 0; i+1 < len(pairs); i += 2 {
		s = strings.ReplaceAll(s, "{"+pairs[i]+"}", pairs[i+1])
	}
	return s
}

// OverlapOjo is the OJO text for a soft-lock overlap (spec section 8).
func (m Messages) OverlapOjo(who, area string) string {
	return m.Format(KeyOjoOverlap, "who", who, "area", area)
}

// ContextTip is the TIP shown when the session is long (spec section 20).
func (m Messages) ContextTip() string { return m[KeyTipLongChat] }
