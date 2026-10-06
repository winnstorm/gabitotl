package gabitotl

import (
	"fmt"
	"regexp"
	"strings"
)

// SecretError reports content that looks like a secret. It never echoes the value.
type SecretError struct {
	Line   int
	Reason string
}

func (e *SecretError) Error() string {
	return fmt.Sprintf("refusing to write: line %d looks like a secret (%s); use env:NAME, keychain:NAME or native instead", e.Line, e.Reason)
}

var (
	sensitiveKey = regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|api[_-]?key|apikey|private[_-]?key|access[_-]?key|credentials?)$`)
	kvLine       = regexp.MustCompile(`^\s*(?:[-*]\s+)?["']?([A-Za-z0-9_.\-]+)["']?\s*[:=]\s*(.*)$`)
	urlPassword  = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.\-]*://[^/\s:@]+:[^@\s/]+@`)
	inlinePwd    = regexp.MustCompile(`(?i)\b(password|pwd)\s*=\s*([^;&\s"']+)`)
	knownTokens  = []struct {
		name string
		re   *regexp.Regexp
	}{
		{"GitHub token", regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}`)},
		{"GitHub fine-grained token", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`)},
		{"GitLab token", regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{16,}`)},
		{"AWS access key", regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)},
		{"API key (sk-)", regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{20,}`)},
		{"Slack token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9\-]{10,}`)},
		{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`)},
		{"JWT", regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}`)},
		{"private key block", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
		{"bearer token", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9_\-.=]{20,}`)},
	}
)

// IsSafeRef reports whether v is an allowed credential reference
// (env:NAME, keychain:NAME or native) or an empty/neutral value.
func IsSafeRef(v string) bool {
	v = strings.Trim(strings.TrimSpace(v), `"'`)
	switch strings.ToLower(v) {
	case "", "null", "~", "true", "false", "native":
		return true
	}
	return strings.HasPrefix(v, "env:") && len(v) > 4 || strings.HasPrefix(v, "keychain:") && len(v) > 9
}

// ValidateNoSecrets scans content line by line and returns a *SecretError for
// the first value that looks like a password, token, key or connection string
// with an embedded password. Only env:/keychain:/native references pass.
func ValidateNoSecrets(content []byte) error {
	for i, line := range strings.Split(string(content), "\n") {
		n := i + 1
		if m := kvLine.FindStringSubmatch(line); m != nil && sensitiveKey.MatchString(m[1]) && !IsSafeRef(stripComment(m[2])) {
			return &SecretError{n, "value of sensitive key " + fmt.Sprintf("%q", m[1])}
		}
		if urlPassword.MatchString(line) {
			return &SecretError{n, "connection string with embedded password"}
		}
		if m := inlinePwd.FindStringSubmatch(line); m != nil && !IsSafeRef(m[2]) {
			return &SecretError{n, "inline password"}
		}
		for _, t := range knownTokens {
			if t.re.MatchString(line) {
				return &SecretError{n, t.name}
			}
		}
	}
	return nil
}

func stripComment(v string) string {
	if i := strings.Index(v, " #"); i >= 0 {
		return v[:i]
	}
	return v
}
