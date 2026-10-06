# Decisions

Notable decisions made while building V1, in short ADR form (context, decision,
consequence). Packages and invariants are in [ARCHITECTURE.md](ARCHITECTURE.md); what
was deferred is in [ROADMAP.md](ROADMAP.md).

## D1. Go, one static binary
- **Decision:** implement in Go and ship a single binary; no runtime, no daemon, no service.
- **Why:** the harness installs into other tools' configs on developer machines; zero
  dependencies and trivial distribution matter more than framework features.
- **Consequence:** `go install` and `make build` are the install paths; packaging is roadmap work.

## D2. Standard library first; `gopkg.in/yaml.v3` is the only dependency
- **Decision:** `flag`, `os/exec`, `encoding/json` and friends from the stdlib; yaml.v3 only for the
  Markdown front matter of `.gabitotl/` files.
- **Consequence:** a small supply-chain surface; a hand-written presenter instead of a TUI library (see D12).

## D3. Core never branches on runtime names
- **Decision:** a runtime is an opaque string; file formats, paths and binary names live only in
  `internal/runtime/{claudecode,opencode,kiro}`. A test fails if core code names a runtime in a conditional.
- **Consequence:** adding a runtime is a new adapter plus one line in `runtime/all`.

## D4. Memory is a contract; Engram is adopted, not copied
- **Decision:** the workflow depends on `memory.Provider` only. Engram is used through its official CLI;
  its database file and `~/.engram` are never opened or modified. A missing Engram is reported, never installed.
- **Why:** the user asked for no database of our own when Engram/Memory MCP already solve memory.
- **Consequence:** `Update`, `SessionSummary` and keyless `CurrentProject` return `ErrUnsupported`
  (no CLI equivalent); recorders fall back to a progress entry.

## D5. A README is not a sufficient "existing project" signal
- **Decision:** a project is "existing" when it has code files, Git history or a manifest. A README alone
  (fresh repositories often start with one) keeps it "new".
- **Consequence:** onboarding asks the new-project questions for empty repos with a README.

## D6. Configuration lives in `.gabitotl/`, learned knowledge lives in memory
- **Decision:** reviewable, versioned Markdown + YAML front matter for configuration; memory for decisions and progress.
  Truth priority: code > `.gabitotl/` > Git > memory > imported docs.
- **Consequence:** nothing needed to operate is hidden only in memory; `local/`, `.bak/`, `tmp/` are git-ignored.

## D7. Secret guard on every `.gabitotl/` write
- **Decision:** `WriteFile` scans content before writing: sensitive keys with literal values, URLs with embedded passwords,
  inline `password=`, and well-known token formats (GitHub, GitLab, AWS, Slack, Google, JWT, private keys, bearer).
  Only `env:NAME`, `keychain:NAME` and `native` references pass. The error never echoes the value.
- **Scope:** `.gabitotl/` files and typed configs. It is a heuristic backstop, not a scanner for the whole repository;
  reports and inventories avoid secrets by construction (paths, ids, counts only).

## D8. Worktrees are sibling directories
- **Decision:** `<repo-parent>/<repo-name>-worktrees/<task-or-user>`; temp directories are refused; removal never forces
  and never touches the main worktree.
- **Why:** each developer needs their own working directory and `.gabitotl/local` identity; siblings keep the repo clean.

## D9. Soft locks only
- **Decision:** overlapping work is detected from active-work files (shared through Git) and memory search and reported as
  an advisory OJO. It never blocks; Git decides real conflicts. A session lock only prevents two users in one working directory.

## D10. History is never rewritten
- **Decision:** force-push, `reset --hard`, amend, rebase, filter-branch and ref updates are rejected by `CheckHistorySafe`
  before reaching Git. Rollback of shared work is a revert commit. Commits must be Conventional Commits and
  attribution trailers are rejected (none are ever added). Every VCS operation is gated by `vcs.md` permissions.

## D11. Backup retention interpretation
- **Decision:** the spec gives a count and an age. A snapshot is kept when it is among the newest `max_snapshots` **or**
  newer than `max_age_days`; it is pruned only when it fails both. Backups never copy `.gabitotl/.bak` or secret-looking
  files, and restoring first saves the current state in a `pre-restore` snapshot.

## D12. Plain presenter now, TUI later
- **Decision:** numbered choices, one question at a time, state symbols (`✓ ! → ×`). All installer logic is in
  `internal/installer`, so a Bubble Tea/Huh UI replaces only `internal/installer/ui`.

## D13. Ownership state, not guesswork, drives removal
- **Decision:** Markdown assets live in marked sections; JSON/JSONC assets are spliced in (comments and formatting preserved)
  and recorded in `~/.gabitotl/state.json`. Uninstall removes only what the state attributes to GabitoTL and restores
  previous values. A document the editor cannot safely scan becomes a manual action instead of a rewrite.

## D14. Retire only after verification; one documented Gentle-AI uninstall form
- **Decision:** snapshot first; Gentle-AI and foreign persona blocks are retired only after every critical check passed;
  failure rolls back. Gentle-AI is removed through its own `gentle-ai uninstall --all`, the only form its CLI documents.
- **Change:** a per-agent form (`uninstall --agent X`) was assumed at first and removed because it is unverified
  (`gentle-ai uninstall -h` documents only `--all`). When `--all` would also remove an agent GabitoTL does not replace,
  GabitoTL does not run it and tells the user to run Gentle-AI's own uninstall manually.

## D15. Context budget is checked once per session
- **Decision:** real token counts when the runtime reports them, otherwise `ceil(runes/4)`. At the threshold (default 75% of
  200k, configurable in `workflow.md`) the checkpoint fires once, saves state to memory and returns the TIP only when memory
  really recorded it; a failure re-arms it and never blocks the user.

## D16. User-facing language is data
- **Decision:** all Spanish wording lives in embedded, replaceable message files; code, docs, comments and tests are English.
  The persona prompt is about 950 estimated tokens, tested against a 1500 budget.

## D17. End-to-end tests drive the real binary against a fake machine
- **Decision:** `internal/e2e` builds the binary once, then runs it with a fixture `$HOME`, a PATH containing only fake
  `engram`/`gentle-ai`/`claude`/`opencode`/`kiro-cli`/`gh` scripts plus symlinks to real `git` and a few coreutils, and temporary repos
  (a local bare repo as origin). Capabilities without a CLI command are exercised through the same internal packages.
- **Consequence:** tests are hermetic and never touch the real `$HOME`; they require a POSIX shell (Linux and macOS).
