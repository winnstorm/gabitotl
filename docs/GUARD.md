# The authorship guard

Authorship of the code belongs to the person who asked for it. GabitoTL therefore
keeps LLM branding (any model, any tool) out of commits, pull requests, code,
comments, docs and memory: it detects it, and removes it or refuses to introduce it.
Everything here is deterministic and offline.

## What it detects (and what it does not)

A finding needs a brand AND an authorship or attribution cue next to it. Plain
technical mentions are never findings: documenting that GabitoTL supports Claude Code,
OpenCode or Kiro, a model id such as `anthropic/claude-sonnet-4`, a config directory
such as `.claude/`, an SDK import or a link to a vendor's documentation.

| Class | What it is | Examples of the shape (never quoted verbatim here) |
|---|---|---|
| `trailer` (A) | Git trailers whose value is an AI identity | `Co-Authored-By`, `Signed-off-by`, `Reviewed-by`, `Assisted-by` with an AI name or an AI vendor address |
| `generated` (B) | Lines that credit a tool for the work | an authorship verb (generated, created, written, drafted, assisted, co-developed...) plus with/by/using plus a brand or the word AI; tags that join AI and generated/assisted/authored; a robot emoji followed by an authorship verb |
| `header` (C) | Author fields | `@author`, `__author__`, `Author:`, `"author":`, `Maintainer:`, `Copyright` naming an AI vendor |
| `boilerplate` (D) | Leaked assistant voice | an assistant introducing itself by its model class or name, lead-ins that present the code as the assistant's reply, closing pleasantries |
| `footer` (E) | Badges, signatures, share links | conversation share links of the AI products, "made with" badges, "sent from <brand>", a bare signature line |

An *AI identity* is a name made only of brand words ("Claude", "Claude Sonnet 5.5",
"GitHub Copilot"), a bot account of an AI tool, or any address at an AI vendor domain.
A person who merely shares a brand word ("Claude Smith <claude@corp.example>") is not one.

Built-in brands (case-insensitive): Claude, Anthropic, ChatGPT, GPT-n, OpenAI, Codex,
Copilot, Gemini, Bard, Qwen, DeepSeek, Kimi, Moonshot, Llama, Meta AI, Mistral, Grok,
xAI, Cursor, Windsurf, Devin, Cline, Aider, OpenCode, Kiro, Gentle-AI, Amazon Q,
Perplexity. Add your own with `branding_extra_terms`.

Each finding carries the file, line, class, the offending line (trimmed and truncated),
and a suggested action: `remove-line` or `manual-review`.

### When a line can be removed automatically

A match is `remove-line` only when everything before it on the line is punctuation or
markup (so the whole line is attribution) and, in source files, the line is a comment.
Anything else is `manual-review`: attribution inside a string, in an assignment such as
an author variable, mixed with prose, or in a comment that opens a block without closing
it. The guard never rewrites code logic.

## Configuration (`.gabitotl/workflow.md` front matter)

```yaml
branding_guard: strip          # strip | block | warn | off   (default: strip)
branding_extra_terms: []       # extra brand names, matched literally, case-insensitive
branding_allow_paths: []       # globs where mentions are legitimate (see below)
guard_hooks: ask               # ask | installed | declined (remembers the hooks answer)
```

Files written before these keys existed keep working: every key has a default.

### Modes, exactly

| Mode | Messages (commit, PR title/body, memory entries) | Files and staged changes |
|---|---|---|
| `strip` (default) | Lines that are only attribution are removed and **reported** (never silent). A line that mixes attribution with content cannot be removed safely, so the operation is refused. | Never rewritten by a hook or a write: the commit (or the write to `.gabitotl/`) is refused; use `guard fix`. |
| `block` | Refused, nothing is edited. | Refused. |
| `warn` | Findings are reported and the operation goes on. | Reported, the commit goes on. |
| `off` | No enforcement. | No enforcement. `guard scan` and `guard fix` still work on demand. |

So `strip` is used where removing a line is safe (a message), and `block` is what happens
everywhere else. The Go library default for a `vcs.Repo` with no policy set is `block`
(the behavior it always had); the CLI always passes the configured mode.

Independently of the mode, the commit validator keeps its original rule: GabitoTL's own
commits never carry a co-author or generated-with trailer, even for a human co-author.

## Where it is enforced

- **`vcs.Commit`, PR creation:** the message / title / body go through the guard first.
- **Git hooks:** `commit-msg` strips or refuses the message; `pre-commit` scans only the
  lines the commit adds (`git diff --cached`), so old content never blocks you.
- **Memory:** `memory.Guard` and `memory.Recorder` strip attribution lines from entries
  before they reach any provider; the team server does the same on save, update and
  session summaries. Lines it cannot strip are refused with `ErrBranding`.
- **`.gabitotl/` writes:** `gabitotl.WriteFile` refuses attribution, like the secret guard.
- **Persona:** the system prompt tells the agent never to add AI branding anywhere and to
  remove any it finds.
- **Runtimes:** see below.

## Git hooks

```bash
gabitotl guard install-hooks      # commit-msg and pre-commit
gabitotl guard uninstall-hooks
gabitotl guard status
```

`gabitotl init` and `gabitotl install` install them by default in a Git project; decline
with `--no-hooks` / `--no-guard-hooks` or answer "n" on a terminal. The answer is remembered
once in `workflow.md` (`guard_hooks`), so nobody is asked twice.

- **Ownership marker:** every hook GabitoTL writes contains `# gabitotl:guard:managed`.
  Only files with the marker are ever rewritten or removed.
- **Chaining:** a hook you already had is renamed `<name>.gabitotl-orig` and called after
  the guard with the original arguments. Uninstall restores it byte for byte. Install is
  idempotent. If both a hook and its `.gabitotl-orig` exist and neither is ours, install
  stops and asks you to resolve it.
- **`gabitotl uninstall`** removes the hooks it installed too (by the marker, never anything else),
  puts a chained hook of yours back byte for byte and lists both in what it removed and kept. It only
  does so for a whole uninstall (not with `--runtime`), and it resets the remembered `guard_hooks`
  answer to `ask`, so a later install offers the hooks again. `guard uninstall-hooks` is unchanged
  and remembers `declined`.
- **`core.hooksPath` and worktrees:** the hooks directory comes from
  `git rev-parse --git-path hooks`, so a custom `core.hooksPath` is honored and linked
  worktrees share the main repository's hooks.
- **Safe by construction:** the wrapper records the absolute path of the binary at install
  time; if it is missing the hook only warns, it never blocks a commit.

## `guard scan` and `guard fix`

```bash
gabitotl guard scan [--staged | --diff REF | --all] [--history [--full]] [--json]   # read-only, exit 1 on findings
gabitotl guard fix  [--dry-run | --yes]
```

`scan` defaults to `--all`: tracked and untracked-but-not-ignored files (a plain walk
outside Git). It never reads `.git/`, binary files, files over 2 MiB, `node_modules`,
`vendor` or `.gabitotl/.bak`. `--history` also audits existing commits (messages and
author/committer identities). The history scan is incremental: the ref tips already scanned, plus the findings in that
history, are remembered per person in `.gabitotl/local/users/<id>/guard-history.json`
(git-ignored). The next scan reads only commits not reachable from those tips and reports
cached plus new findings. The cache is ignored (one full scan) when the detection rules or
`branding_extra_terms` change, or when it is unreadable. `--full` rescans everything; with
no active identity every scan is full.

`fix` removes only the lines that are pure attribution, after a backup under
`.gabitotl/.bak/<user>/<timestamp>_guard-fix/`. It refuses to rewrite anything ambiguous
(reported as manual review) and never rewrites a file it could not back up (for example a
secret-looking one). **History is never rewritten**: commits that already carry attribution
are reported with a note, and what to do about them is a decision for the people who own
that history.

## False-positive policy and the allowlist

Precision beats recall: a pattern must have a brand plus an authorship cue, and each class
has negative tests (`internal/branding/testdata/corpus.txt`). If something legitimate is
flagged, prefer these, in order:

1. Reword so the line is not an attribution (state the fact, not "made with X").
2. Build the string at runtime in a test (string concatenation).
3. Allow the path: `branding_allow_paths` takes gitignore-like globs (`*` stays in a
   segment, `**` crosses segments, a pattern without `/` matches at any depth, a trailing
   `/` is a whole directory), for example `docs/runtimes/` or `**/testdata/`.

Allow narrowly. GabitoTL's own repository allows only the detector corpus
(`internal/branding/testdata/`) and passes `gabitotl guard scan --all` with zero findings.

## Runtime attribution settings

The tools themselves can add attribution, so the adapters switch it off at the source
through the ownership engine (managed keys, idempotent, removed on `uninstall` /
`runtime remove`, user values never overwritten):

| Runtime | Setting | Status |
|---|---|---|
| Claude Code | `attribution.commit`, `attribution.pr` set to an empty string, `attribution.sessionUrl` set to `false` in `~/.claude/settings.json` (project scope: `.claude/settings.json`) | The `attribution` object and its three fields are documented at <https://code.claude.com/docs/en/settings-reference> (fetched 2026-10); `includeCoAuthoredBy` is deprecated there. **ASSUMPTION:** an empty string hides the commit trailer and the PR line (long-standing behavior; one fetch of the page listed `false` for it and another could not confirm the exact value). |
| OpenCode | none | No option found at <https://opencode.ai/docs/config/> (2026-10). Nothing is invented; hooks and persona cover it. |
| Kiro | none | No option found at <https://kiro.dev/docs/cli/> (2026-10). Same. |

If you already set `attribution.*` yourself, GabitoTL keeps your value (PRESERVE) and
`doctor` says so.

## doctor

`gabitotl doctor` has an "Autoría/branding" section (read-only): guard mode, hooks
installed/chained/declined, each runtime's own attribution setting, a scan summary and the
number of existing commits that carry attribution.
