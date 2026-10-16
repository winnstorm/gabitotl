# Decisions

Notable decisions made while building V1, in short ADR form (context, decision,
consequence). Packages and invariants are in [ARCHITECTURE.md](ARCHITECTURE.md); what
was deferred is in [ROADMAP.md](ROADMAP.md).

## D1. Go, one static binary
- **Decision:** implement in Go and ship a single binary; no runtime, no daemon, no service.
- **Why:** the harness installs into other tools' configs on developer machines; zero
  dependencies and trivial distribution matter more than framework features.
- **Consequence:** `go install` and `make build` are the install paths; packaging is roadmap work.

## D2. Standard library first; yaml.v3 and the Charm stack are the only dependencies
- **Decision:** `flag`, `os/exec`, `encoding/json` and friends from the stdlib; `gopkg.in/yaml.v3` only for the
  Markdown front matter of `.gabitotl/` files; Bubble Tea, Lip Gloss and Huh only in `internal/tui` (see D18).
- **Consequence:** a small supply-chain surface; the plain presenter stays for non-interactive use (see D12).

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
- **Why:** each developer needs their own working directory; siblings keep the repo clean.

## D9. Soft locks only
- **Decision:** overlapping work is detected from active-work files (shared through Git) and memory search and reported as
  an advisory OJO. It never blocks; Git decides real conflicts. The old exclusive session lock was replaced by an advisory registry (D26).

## D10. History is never rewritten
- **Decision:** force-push, `reset --hard`, amend, rebase, filter-branch and ref updates are rejected by `CheckHistorySafe`
  before reaching Git. Rollback of shared work is a revert commit. Commits must be Conventional Commits and
  attribution trailers are rejected (none are ever added). Every VCS operation is gated by `vcs.md` permissions.

## D11. Backup retention interpretation
- **Decision:** the spec gives a count and an age. A snapshot is kept when it is among the newest `max_snapshots` **or**
  newer than `max_age_days`; it is pruned only when it fails both. Backups never copy `.gabitotl/.bak` or secret-looking
  files, and restoring first saves the current state in a `pre-restore` snapshot.

## D12. Plain presenter first, TUI on top (superseded in part by D18)
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

## D18. Charm v2 stack for the TUI, presentation only
- **Decision:** `internal/tui` uses Bubble Tea, Lip Gloss and Huh at their current stable majors, which
  live under the `charm.land` module paths: `charm.land/bubbletea/v2` v2.0.10, `charm.land/lipgloss/v2` v2.0.6,
  `charm.land/huh/v2` v2.0.3 (the old `github.com/charmbracelet/*` paths are the v1 line). Huh forms
  (one question each) are embedded in the Bubble Tea model; menus, plan and progress are plain Bubble Tea
  with Lip Gloss. `bubbles` and `teatest` are not used directly.
- **Why:** the installer spec (section 3) names this stack; v2 is what the official docs show. Tests drive
  `Update` with messages and run the returned commands, so no terminal is needed.
- **Boundaries:** the TUI reaches `internal/installer` only through the `Backend` interface (detect, build plan, run)
  and reads the inventory and plan as data (`ui.InventorySections`, `ui.PlanGroups`); it holds no business rules.
  All wording stays in `messages_es-AR.txt`. The TUI opens only when stdin and stdout are terminals;
  `--no-tui`, `--yes`, `--dry-run`, pipes and `migrate` keep the plain flow.
- **Cancellation:** `installer.Run` now honors context cancellation between phases and runs the retire phase to
  completion (point of no return). `ctrl+c` while applying cancels the context and waits for the pipeline's own
  rollback; the TUI never exits mid-apply.
- **Consequence:** about 25 more modules in `go.sum`; `NO_COLOR` strips every style, so state is always carried by symbols.

## D19. Our own Memory MCP server on a managed, isolated PostgreSQL; the actor comes from the token
- **Decision:** team memory is served by GabitoTL's own Go MCP server (`internal/memory/server`, official Go SDK) over a
  `store.Store`, implemented for PostgreSQL (`pgx/v5`) and in memory. The PostgreSQL is a container GabitoTL creates and
  labels itself (`internal/pgmanaged`), not an embedded database and not the user's PostgreSQL. Over HTTP the actor of every
  write is the owner of the bearer token; a request naming another actor is rejected.
- **Why a server of our own:** the spec's contract is actor, branch, task, commit and soft-lock queries on every entry; a
  third-party Memory MCP has its own schema (spec 20: never assume it matches). Owning the tools gives stable names and
  JSON schemas, per-user identity and `memory_active_work` for the soft lock. Engram stays the solo provider.
- **Why a managed container instead of embedded or the user's PostgreSQL:** an embedded database cannot be shared by a
  team, and sharing the user's PostgreSQL could interfere with it. A labeled container with its own volume and network can
  be proven isolated: every operation checks the label first and refuses foreign resources, the port is loopback-only and
  never 5432, and removal keeps the data unless purged. Image `postgres:17.11-alpine`: 17 is supported until 2029 and keeps
  the data directory at `/var/lib/postgresql/data`; 18 is current but moved it, so adopting it is a deliberate migration.
- **Why the token decides the actor:** a client-supplied actor would make "who changed this" unreliable in team mode. Tokens
  are `gbt_<id>.<secret>`; only the SHA-256 of the secret is stored, compared in constant time, shown once, revocable at
  once. `legacy` is never an authenticated actor (imports go through the store directly). stdio, a single trusted local
  client, uses an actor fixed at startup.
- **Search:** PostgreSQL full text with the `simple` configuration, so Spanish and English text behave alike (no stemming);
  prefix matching recovers inflected forms. Title outweighs topic/task, which outweighs the body.
- **Stateless HTTP:** every tool is request/response, so streamable HTTP runs stateless with JSON responses (no sessions, no
  server streams), which keeps timeouts, rate limits and proxies simple. TLS is optional; plain HTTP on a non-loopback
  address is refused unless `--allow-insecure-remote` declares a TLS-terminating proxy.
- **Consequence:** adds `pgx/v5` and `go-sdk` (14 modules in `go.sum`, two of them test-only helpers of pgx). Default tests never need Docker; Docker and PostgreSQL
  suites are opt-in (`GABITOTL_TEST_DOCKER=1`, `GABITOTL_TEST_PG_DSN`).

## D20. Runtimes get one stdio entry; a bridge forwards to the server
- **Decision:** in team mode every runtime registers ONE managed MCP entry that runs `gabitotl memory mcp`. That
  command is a stdio MCP server built from the same tool descriptors as the real server; it forwards each call over
  streamable HTTP with the developer's token (read from an `env:NAME` reference at call time, with the 0600
  `memory-client.env` as fallback for runtimes that do not inherit the shell environment).
- **Why:** the three runtimes differ in how they configure remote MCP servers, auth headers and environment
  expansion; a stdio command is the one shape all of them support, and the token never has to be written into a
  runtime config. The bridge has no identity or store of its own, so the server stays the only authority.
- **Switching providers:** an adapter plans the removal of the other provider's entry (REMOVE-AFTER-VERIFY) only
  when the ownership state says GabitoTL created that exact key; entries the user wrote are never touched.
- **Consequence:** `gabitotl` must be on the PATH the runtime uses; the bridge resolves its URL and token from the
  project's `memory.md` (or `--url`/`--token-env`).

## D21. Tokens are project-scoped; "no scope" is never "everything"
- **Decision:** a token carries either an explicit project allowlist (`--projects a,b`) or an explicit all-projects flag
  (`--all-projects`, warned); creating one with neither is a usage error. The server enforces it on every tool.
  Tokens created before the change stay all-project (schema migration 0002); there is no in-place edit.
- **Why:** the first server version trusted anyone with a token on every project and told operators to run one
  server per trust boundary. Scoping makes one server per team workable. Failing closed on an unreadable scope and on an
  update that names no project (the store would match any project) keeps the check unbypassable.
- **Proxy:** the per-address authentication throttle can trust one header (`--trusted-proxy-header`, off by default,
  last entry wins) because behind a proxy every client otherwise shares one address.

## D22. The Engram migrator writes through the store; the actor is never invented
- **Decision:** `engrammcp` imports the official export through a store-level `Importer` keyed by a stable source
  reference. It requires a non-empty safety export, skips deleted and personal-scope observations, never overwrites a
  taken `topic_key`, verifies counts and a content-hash sample, and keeps its report free of content. The actor is the
  author recorded by a GabitoTL trailer, else `legacy` (`source: engram/gentle-ai`).
- **Why through the store:** `legacy` can never authenticate (D19), and a re-runnable import needs an idempotency key and
  the original timestamps, which the tools deliberately do not accept. Personal-scope notes would otherwise become
  visible to the whole team.
- **Pair name:** the migrator is registered for (`engram`, `memory-mcp`): `memory-mcp` is the provider name in
  `memory.md` and, since D19, it means this repository's own server.
- **Schema honesty:** the export schema was read from the upstream source (summarized by the fetch tool) and the
  installed `engram` binary was not inspected; what is verified and what is assumed is listed in the package doc.

## D23. Team memory is set up by the installer, and a rollback never deletes data
- **Decision:** the host role runs the managed PostgreSQL plan inside the installer pipeline (snapshot first, verify,
  roll back). A rollback removes only the container and network the run itself created and keeps the volume and the
  credentials; a pre-existing server is left alone. No container engine means the plan says so and offers the client
  role or Engram; GabitoTL never installs Docker.
- **Why:** a failed install must be retryable without losing data, and the isolation promise ("never touch a PostgreSQL
  you already run") holds only if rollback is as careful as install.

## D24. Authorship belongs to the requester; the guard targets attribution, not mentions
- **Decision:** LLM branding that claims authorship (co-author trailers and identities, "generated with/by" lines,
  author headers, assistant boilerplate, badges and share links) is kept out of commits, PRs, files, `.gabitotl/`
  and memory, for any model or tool. The detector (`internal/branding`) needs a brand plus an authorship cue; a
  plain technical mention (a runtime we document, a model id, a config dir, an SDK import) is never a finding.
- **Why:** the person who asked for the work owns it. Tools add their own attribution by default, so one layer is
  not enough: the guard sits at the persona, the runtime settings, the Git hooks, `vcs.Commit`/PR creation, memory
  and `.gabitotl/` writes (defense in depth), with `scan`/`fix` for what is already there. Precision matters more
  than recall: a guard that cries wolf on documentation gets switched off.
- **Strip vs block:** `strip` (the default) edits only where removing a line is safe, that is free text such as a
  commit message, PR text and memory entries, and always reports what it removed. Files and staged changes are never
  rewritten by a hook or a write: they are refused (`block`) and fixed on purpose with `guard fix`, which backs up
  first and leaves ambiguous matches (attribution mixed with code or prose) for manual review. A mixed line in a
  message is refused too. `warn` and `off` exist for people who disagree; the zero-value policy of the Go library is
  `block`, the behavior it always had, while the CLI passes the configured mode.
- **No history rewrite:** commits that already carry attribution are reported (`guard scan --history`, `doctor`),
  never rewritten. This is the same rule as D10 (history is never rewritten); the owners of that history decide.
- **Hooks never replace yours:** a user hook is moved to `<name>.gabitotl-orig` and chained, and uninstall restores
  it; ownership is a marker in the file. A missing binary warns instead of blocking.
- **Runtime settings:** only documented settings are written. Claude Code's `attribution` object is managed
  (empty commit and PR text, no session link; the empty-string value is an ASSUMPTION, see docs/GUARD.md); OpenCode
  and Kiro have no documented option, so none is invented.

## D25. Identity comes from the OS user; collisions are advisory
- **Decision:** the active identity is the slug of the OS username (a Windows `DOMAIN\` prefix and a `user@domain`
  suffix are dropped); `GABITOTL_USER` overrides it. There is no identity file and no first-run question. `Active`
  silently creates the shared profile `.gabitotl/users/<id>.md` (display name from `git config user.name`, else the
  id) and never rewrites an existing one. `--user`/`--user-name` still create an explicit profile. A leftover
  `.gabitotl/local/user.md` is ignored. Collisions between people on the same project are advisory: warn, never block.
- **Why:** several people can share one working directory, and each must be told apart without anyone typing a name.
  The OS user is already the boundary the operating system enforces.
- **Consequence:** the same person on another machine is the same id only if the usernames match; otherwise they are a
  different user, by design (set `GABITOTL_USER` to align them).
- **Per-person local state:** `session.md` and the runtime override move to `.gabitotl/local/users/<id>/` so two OS users
  sharing one directory never overwrite each other. A legacy `local/runtime.md` is read as a fallback, never migrated
  or deleted. Backups already live in `.bak/<id>/`.

## D26. An advisory session registry replaces the exclusive lock
- **Decision:** `workdir.lock` and `BusyError` are gone. Every live session registers
  `.gabitotl/local/sessions/<id>-<pid>.yaml` (user, pid, started, last_seen, optional task, branch, areas, runtime);
  each person writes only their own files, the directory is group-writable (0o775, files 0o664, subject to the umask).
  Other people's live sessions are shown as a short advisory line ("ana is also active in this directory since 12 min
  ago (task auth-refactor)"); `gabitotl worktree add` is mentioned only as an optional tip. A directory that cannot be
  written is a warning, never an error.
- **Liveness:** a session is live when its anchor PID exists (EPERM counts as alive) and `last_seen` is within
  `SessionTTL` (12 h). Dead or expired entries are pruned lazily by whoever reads. Where PIDs cannot be probed
  (non-Unix) only the TTL applies.
- **Anchor:** there are no agent-runtime hooks in GabitoTL (the guard hooks are Git hooks, children of a short-lived
  `git`), and most CLI commands exit in milliseconds, so their own PID proves nothing. Long-lived entry points
  (`memory serve --stdio`, `memory mcp`, which an agent runtime spawns for the whole session) use their own PID,
  refresh `last_seen` every 10 min and unregister on exit. Short-lived ones (`work start|update`) use the parent PID
  (the shell or agent that ran them) and rely on the TTL when that parent is a throwaway shell.
- **Where it speaks:** notices go to stderr (`work start|update`, both stdio servers: stdout is the MCP protocol),
  to stdout in status views (`work status`, `gabitotl`). Git hooks (`guard commit-msg|scan --hook`) neither register
  nor print: their output is shown inside `git commit` and they have no stable anchor.
- **Why:** several people in one working directory is normal; blocking the second one only pushed them to work around
  the lock. Git still decides real conflicts (D9).

## D27. `who` and agent-agnostic session commands
- **Decision:** `gabitotl who` is a read-only, advisory view that merges three scopes: live sessions of this directory,
  live sessions of every other worktree (`git worktree list`, each read from that worktree's own
  `.gabitotl/local/sessions`) and pushed state (`.gabitotl/work/<user>/*.md` active tasks, plus `ActiveWork` of the
  memory-mcp provider, last 24 h). Each row carries its scope and source. It always exits 0: a source that cannot be
  read (not a Git repository, memory unreachable) becomes a warning; Engram has no `ActiveWork` and is skipped silently.
- **De-duplication:** rows of the same user are one when they share a task, or when one side has no task and both share
  a branch, or when both are bare sessions in the same place. The most specific source wins (this directory, another
  worktree, work file, memory) and only fills what it lacks (branch, areas, runtime); the earliest start is kept.
- **Session commands:** `gabitotl session start|touch|end` register, refresh and remove the active identity's session so
  any agent runtime can show presence whatever memory provider the project uses (closes the gap that an Engram-only
  project had no automatic registration). The anchor is `--anchor-pid` or the parent process. `start` prints the notice
  for other live people once on stdout (so a hook can pass it to the agent as context) and nothing else. Outside an
  initialized project, or without an identity, all three succeed silently; an unwritable sessions directory is a
  warning; none of them blocks. `touch` never creates a session.
- **Hooks:** no runtime adapter writes agent hooks yet. The Claude Code snippet is documented in the README as a
  manual option. Wiring it into the installer needs a managed merge of object entries in `hooks.<Event>` arrays
  (`engine.PlanKey` would replace the whole array and `PlanItem` handles strings only), see the README.
- **Why:** presence is only useful if it covers every way of working (agents, shells, other worktrees, other machines)
  and only trustworthy if it can never get in the way (D25, D26).
