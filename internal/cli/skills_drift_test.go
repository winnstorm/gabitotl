package cli

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/skills"
)

// cmdRe finds `gabitotl <command> [<word>]` where the text is in backticks or a
// code block. Flags and placeholders end the match.
var cmdRe = regexp.MustCompile("gabitotl ([a-z][a-z-]*)(?: ([a-z][a-z-]*))?")

// subcommandGroups lists the commands that dispatch a subcommand and the ones
// they accept. A drift test must fail when a skill names one that does not exist.
var subcommandGroups = map[string][]string{
	"vcs":      {"status", "branch", "setup", "login", "commit", "push", "pr", "merge", "ship"},
	"work":     {"start", "update", "status", "overlap"},
	"session":  {"start", "touch", "end"},
	"worktree": {"add", "list", "remove"},
	"backup":   {"create", "list", "restore", "prune"},
	"index":    {"status", "setup"},
	"skills":   {"list", "enable", "disable"},
	"memory":   {"doctor", "search", "context", "compact", "use", "server", "serve", "token", "mcp", "migrate"},
}

func TestSkillsOnlyMentionRealCommands(t *testing.T) {
	reg := commands()
	for _, s := range skills.All() {
		inCode := false
		for _, line := range strings.Split(s.Content, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inCode = !inCode
				continue
			}
			text := line
			if !inCode {
				text = strings.Join(backticked(line), " ")
			}
			for _, m := range cmdRe.FindAllStringSubmatch(text, -1) {
				cmd, sub := m[1], m[2]
				if _, ok := reg[cmd]; !ok {
					t.Errorf("%s mentions `gabitotl %s`, which is not a command", s.Name, cmd)
					continue
				}
				subs, dispatches := subcommandGroups[cmd]
				if !dispatches || sub == "" {
					continue
				}
				if !slices.Contains(subs, sub) {
					t.Errorf("%s mentions `gabitotl %s %s`, which is not a subcommand", s.Name, cmd, sub)
				}
			}
		}
	}
}

func TestSubcommandGroupsMatchTheCLI(t *testing.T) {
	root := t.TempDir()
	for group, subs := range subcommandGroups {
		r := runIn(t, "", false, root, group, "definitely-not-a-subcommand")
		for _, sub := range subs {
			if !strings.Contains(r.err, sub) {
				t.Errorf("`gabitotl %s` does not list %q in its usage: %q", group, sub, r.err)
			}
		}
	}
}

func TestSkillsNeverAskAboutPRSizeExceptions(t *testing.T) {
	for _, s := range skills.All() {
		if strings.Contains(strings.ToLower(s.Content), "size exception") {
			t.Errorf("%s mentions a size exception", s.Name)
		}
	}
}

func backticked(line string) []string {
	var out []string
	parts := strings.Split(line, "`")
	for i := 1; i < len(parts); i += 2 {
		out = append(out, parts[i])
	}
	return out
}
