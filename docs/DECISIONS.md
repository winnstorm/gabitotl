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
  The persona prompt is about 1400 estimated tokens, tested against a 1500 budget. It includes a "Noisy terminal" rule: after a shell command fails or returns noisy output (multiline or quoted commands, PowerShell/cmd escaping), the agent writes a short Python script in the git-ignored `.gabitotl/tmp/` instead of retrying, on any OS.

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

## D28. The memory provider is chosen per project; one bridge entry routes by project
- **Decision:** both providers (Engram, local; memory-mcp, the shared server) are available on every machine and the choice lives
  in `provider` of each project's `.gabitotl/memory.md`. Every runtime gets the same registration, whichever provider a project
  uses: the `gabitotl-memory` entry running `gabitotl memory mcp`, plus Engram's own `engram` entry when the binary is on
  `PATH`. `rt.MemoryFor(provider, engramPresent)` returns `(bridge, extra, retire)`; a known provider always yields the same
  setup, `none` configures nothing.
- **Path taken, and why:** the existing bridge did NOT route (it started only for memory-mcp projects, from the memory.md read
  once at startup, and refused Engram projects). It now reads `.gabitotl/memory.md` from the project it runs in (Git top-level
  of its working directory, or `--root`) on EVERY call (`cli.memoryRouter`): memory-mcp projects are forwarded to the team
  server with the project's URL, token reference and key; Engram projects are served through the engram CLI provider
  (search, save, current project, doctor). A directory without a usable memory.md still starts the server and every tool answers
  `no memory provider configured for this project`. Explicit `--url` keeps the old plain-bridge behavior. This is the smallest
  change that makes a single GLOBAL registration per-project; the fallback of "global registers nothing, `--scope project`
  registers the project's provider" was not needed. Reading per call also means switching a project's provider needs no restart.
- **Engram through the bridge is a subset:** the CLI has no update and no session summary, so `memory_update` reports
  `unsupported` (save again with the same `topic_key`), a session summary is stored as a progress entry (as `memory.Recorder`
  does) and `memory_active_work` is team-only. This is why Engram's own entry is kept next to the bridge: Engram-backed
  projects keep its full native toolset. The trade-off is that a team project also sees `engram` tools in that runtime; the
  persona and the project's memory.md decide which one to use.
- **Retirement:** `RetireMemory` no longer removes a provider another project may use. The only obsolete entry is Engram's own
  once its binary is gone, and the adapter removes it only when the ownership state says GabitoTL created that exact entry (user
  entries are never touched; backups and rollback are unchanged). `PlanRetireServers` takes the list of entries that stay
  configured, so a configured entry can never be retired.
- **Availability reporting:** `doctor` reports Engram (binary, `engram doctor`, data) and the team server (configured,
  reachable) as separate lines; install verification reports Engram as its own informational check. A missing Engram is a
  warning only for a project that uses it (an error only if its data exists but the binary does not) and an informational line
  otherwise; an unhealthy Engram in a team project is a warning. The installer never downloads or installs Engram; it prints
  the repository URL as a hint.
- **Migration for existing installs:** the next `gabitotl install`/`runtime apply` adds the bridge entry (Engram users) or
  Engram's entry (team users with the binary); nothing GabitoTL created earlier is removed while Engram exists. The default
  provider written by `init` is unchanged; the explicit per-project choice UX is a separate task.

## D29. The provider is chosen by a question per project; changing it is a migration
- **Decision:** the memory provider of a project is picked by one question, "do you work on this project alone or with other
  people?", asked by `init` and by the installer (plain and full-screen flows share the wording): alone is Engram, with others
  is memory-mcp. For team memory the installer's existing team setup asks only what it needs (host or client, server URL, token
  variable, Engram import). A project that already exists on Engram is asked once, when the interactive flow is re-run, whether
  it now works with other people. The commands are the non-interactive equivalent: `--provider` (init), `--memory` (install)
  and `gabitotl memory use <engram|memory-mcp> [--migrate] [--yes] [--dry-run] [--role ...]`. Nothing prompts without a
  terminal or with `--yes`.
- **Change is a migration, through the existing pipeline:** there is no second migrator. `memory use` and the interactive
  change run the install flow with the provider fixed, so they inherit the plan with the entry count and `--dry-run`, the
  snapshot that holds the previous `memory.md`, the tested Engram migrator (safety export, no duplicates, verified, Engram
  never modified), idempotent re-runs, and the rollback that restores `memory.md` to the old provider when anything fails.
  Entries imported before a failure stay in the team memory (nothing is deleted); the result reports the counts.
- **Direction:** Engram to memory-mcp only. memory-mcp to Engram has no migrator, so it is refused (`ErrNoReverseMigration`,
  `memory use engram`) and leaves memory.md untouched rather than stranding the team's memory. Importing needs the database, so
  `--migrate` is a host-only step; a client switch records the connection and the import runs on the host.
- **Collisions:** `memory.md` is committed, so a change reaches teammates when they pull. One advisory line says so after a
  change; it never blocks. `doctor` reports the chosen provider and warns when the mode and provider disagree or team memory
  has no server or token reference.
- **Also:** a host without `--user` now uses the OS-user identity (D25) instead of failing.

## D30. Team memory settings split: shared vs per machine
- **Problem:** the team setup wrote facts of one machine into the committed `memory.md` (`role`, `instance`, `token`,
  `postgres_connection` and, for a host, `http://127.0.0.1:8765/mcp`). The first person to configure a project therefore
  committed a loopback address and `role: host` that every teammate inherited, although a client needs only the server URL
  and its own token.
- **Decision:** two files. The committed `.gabitotl/memory.md` keeps what every teammate needs: `mode`, `provider`,
  `backend`, `project_key` and `url`, the TEAM-REACHABLE server address, never a loopback or unspecified host. The
  per-person `.gabitotl/local/users/<id>/memory.md` (git-ignored, next to the runtime override and session) keeps `role`,
  `instance`, `token` (an env reference), `postgres_connection` and an optional `url` override: a host reaches its own
  server on 127.0.0.1 while the shared file holds the public address. Only references are stored, as before.
- **Merged view:** `gabitotl.MemoryConfig` stays the one struct the callers read. `LoadMemory` merges shared and local (local
  wins per field); `SaveMemory` splits: a loopback `url` goes to the local file only, a reachable `url` to the shared one,
  and `SaveMemoryTeamURL` lets a host write the team address and its local address in one call (a loopback team address is
  refused with `ErrLoopbackTeamURL`). The person id is `gabitotl.ActiveUserID` (OS user, `GABITOTL_USER` overrides); it
  lives in package `gabitotl` and `identity` delegates to it, because `identity` imports `gabitotl`. With no usable id the
  settings stay in the shared file, as before.
- **Legacy files:** a `memory.md` that still carries per-machine fields keeps working: they are the fallback for what the
  local file lacks. The next write of memory settings moves them to the local file and removes them from the shared one
  (a legacy loopback `url` becomes the local override), preserving the Markdown body and unknown front matter keys. The file
  is never deleted. The local file is part of the install snapshot, so a rollback restores it too.
- **Init:** `init` no longer writes `postgres_connection` into the committed file for memory-mcp; it goes to the local one.
- **Host flow:** the installer asks a host (interactive questions and TUI form, optional) for the address teammates use and
  validates that it is not loopback; `--memory-url` is the same answer for non-interactive runs. It is written to the shared
  file while the host's own process keeps `http://127.0.0.1:8765/mcp` as its local override. Without an address the host
  is set up locally, nothing is shared, and one line says teammates cannot connect until `gabitotl memory use memory-mcp
  --role host --url URL` publishes one (`memory use` with team flags on a team project re-runs the idempotent setup). A
  loopback `--memory-url` is an error; omitting it is the local-only mode (no extra flag).
- **Teammate flow:** a project whose committed `memory.md` says memory-mcp with a reachable `url` and whose local file is
  missing is recognized by `install` and `doctor`: role client, no role or url question, only the token variable name; an
  unset variable prints how to get a token from the host. `doctor` also warns about a committed loopback `url` and about
  per-machine fields still in the shared file, with the exact fix.
- **Warning fix:** the skipped-import message is a coded notice (`Result.Notices`, `note.import-skipped-*`) with advice by
  role: hosts are told to start the server, clients to ask the host for a token when their variable is unset.

## D31. Gentle AI's Kiro assets are retired into a restorable backup
- **Problem:** Gentle AI writes always-on assets into Kiro (`steering/gentle-ai.md` with `inclusion: always`, skills,
  agents). Next to GabitoTL's steering file Kiro then runs two personas at once, and the existing persona detector only
  matches blocks named "persona" and only runs for Claude Code.
- **Decision:** with `Options.RetireGentleKiro` the installer moves those assets, and only those, into the snapshot of the
  install and removes them after verification. It is not `gentle-ai uninstall` (D14 is unchanged): Gentle AI itself,
  its state and its other agents are untouched.
- **Ownership proof:** the steering file by name; a skill directory only when its `SKILL.md` front matter has
  `metadata.author: gentleman-programming` (BOM, CRLF and quotes tolerated; unparsable means not Gentle's), and then
  only its Markdown files (top level and `references/`); `skills/_shared` by its known file names; agents by Gentle's
  names plus the plain file names in `agents/.gentle-ai-native-agent-ownership.json` and the manifest itself. Anything
  else inside those directories (a user file in a Gentle skill, a user agent, `settings/`, MCP entries) is kept and
  reported. Directories are removed only when the removal emptied them.
- **Order:** the files are copied under `gentle-kiro/<base>/` in the snapshot, each copy verified by size and SHA-256,
  before any mutation; a failed copy aborts the run with nothing removed. Removal runs in the retire phase, after the
  critical checks, re-verifies every saved copy first, and skips (and reports) a file whose content changed since the
  plan. A failed run rolls back as usual and removes nothing it had not already verified.
- **Restore:** the copies are ordinary manifest files, so `gabitotl restore <id>` and the rollback reuse the existing
  mechanism and older snapshots stay restorable. Restore now refuses to write a saved copy whose hash no longer matches
  the manifest.
- **Scope:** the global tree (`KIRO_HOME` or `~/.kiro`) and the workspace `.kiro`, found through the adapter
  (`ForeignAssetRoots`), so the core never branches on runtime names (D3). The question, flag and doctor report belong to
  the presenters.
- **Interaction rules (ask, never assume):** the retirement happens only on an explicit yes. On a terminal the plain
  installer and the full-screen one show what was found (steering yes/no, skills, agents, shared references), what stays
  (the user's files inside those folders, MCP entries, Gentle AI itself) and ask; the default answer is no (Enter, an
  unknown answer or closed input keep both) and the TUI cursor starts on "keep both". A yes replans, so the plan lists
  exactly what moves before the final confirmation. Without a terminal, or with `--yes` alone, nothing is retired and
  nothing is asked: `NoteGentleKiroPresent` names `--retire-gentle-kiro`, the only non-interactive way to say yes (also
  accepted by `migrate gentle-ai`). `--dry-run` never asks and renders the plan as the flag would. Quitting before apply
  changes nothing. After a retirement the inventory is empty, so a re-run asks nothing and creates no snapshot.
- **Doctor:** when a Kiro runtime managed by GabitoTL has its own `steering/gabitotl.md` and Gentle AI's files are still in
  the same tree, `doctor` adds a warning (never an error, so the exit code does not change) that two always-on instruction
  sets apply, with `gabitotl install --runtime <kiro-cli|kiro-ide> --retire-gentle-kiro`. With nothing in common it says
  nothing.
- **Result:** the report prints the number of files moved, the snapshot id with `gabitotl restore <id>`, and briefly the
  files skipped (changed since the plan) and the user's files kept.

## D32. GabitoTL's skills are whole files it owns, installed per runtime
- **Problem:** GabitoTL could only edit sections, JSON keys and array items inside files. Agent skills
  (`<skills dir>/<name>/SKILL.md`) are whole files, and there was no way to create one, update it safely or remove it
  without risking a file the user wrote or edited.
- **Decision:** a new engine action, `OpWriteFile` (`PlanFile`, `PlanSkills`), owns a file entirely. The text lives in
  `internal/skills` (embedded `files/<name>/SKILL.md`, English, `metadata.author: gabitotl`); the installer turns it
  into `runtime.Asset` values in `Options.Skills` and every adapter plans one file per skill under its own skills
  directory, so the core never branches on runtime names (D3).
- **Planning rules:** absent is CREATE; identical is NO-ACTION; different is MODIFY only when the ownership state says
  GabitoTL wrote the file and its hash still equals the recorded one; any other different file is PRESERVE with a
  manual action. A user's file is never overwritten, and an identical file nobody recorded is never claimed.
- **Ownership:** `ownership.Entry` gains `whole_file` and `created_dirs` (both optional, so old `state.json` files
  load unchanged). Uninstall deletes the file only if its hash still equals `hash_after`; otherwise it reports the
  file as a manual action and keeps the entry. The directories GabitoTL created are removed afterwards, innermost
  first, and only if empty. Shared files (Kiro CLI and IDE) wait for the last owner.
- **Backup and verify:** the existing global backup copies the target before a MODIFY; `Apply` writes atomically
  (temp file and rename, directories 0755, file 0644) and verifies the content hash.
- **Not Gentle's:** the Gentle AI retirement (D31) matches `metadata.author: gentleman-programming`; GabitoTL skills use
  `gabitotl`, and a test proves they are never selected.
- **Open point:** the OpenCode skills directory is derived from Gentle AI's steering text and is not confirmed by
  OpenCode's documentation (ASSUMPTION in the adapter's package doc).

## D33. Delivery settings are asked once and split between the team and each person
- **Problem:** the only Git question was a yes/no for a NEW project, written into the shared `vcs.md` as "everything
  allowed". It never confirmed the platform, had no merge permission, and could not tell what the team agreed on from
  what one person wants. Asking about PR size or exceptions was never acceptable.
- **Decision:** `VCSConfig` is a merged view (the same design as D30). The committed `.gabitotl/vcs.md` holds the team
  settings: `provider`, `permissions` (now with `merge`), `merge_method` (default squash), `delete_branch` (true),
  `draft_prs` (false), `base_branch` (empty: the repository default), `pr_budget_lines` (400), `chain` (`stacked`) and
  the marker `configured`. The git-ignored `.gabitotl/local/users/<id>/vcs.md` holds the `auth` reference and optional
  permission overrides; the local value wins. Every new key is optional, so older files load unchanged and a legacy
  `auth` in the shared file moves to the person's file on the next write.
- **Configured:** `configured: true`, or (a file from before the marker) any permission other than `ask` or
  `remote_actions: automatic`. A file that is still the defaults is not configured.
- **Questions:** at most five, only while the project is not configured (new projects and existing ones without the
  marker), in the plain installer, the TUI and `gabitotl vcs setup`: platform (the one inferred from `origin` is the
  default), automation (everything / up to the PR, default / ask each time), merge method and branch deletion (only
  when merge is automatic) and draft PRs (unless everything asks). There is no question about size, budgets, chaining
  or exceptions: those are settings with defaults, changed by editing `vcs.md`. Work over the budget is sliced into
  stacked PRs without asking.
- **Flags:** `--vcs-provider`, `--vcs-automation=all|until-pr|ask`, `--vcs-merge-method`, `--vcs-delete-branch`,
  `--vcs-draft` on `install`, `migrate` and `vcs setup`. `--vcs-automation` settles the questions without a terminal;
  `--yes` and a missing terminal never ask and never mark anything configured.
- **Where an answer goes (`SaveDelivery`):** the team file when it is new or unconfigured, when a team-wide setting is
  answered, or with `vcs setup --shared`; otherwise (the team already answered and the person only picks how much to
  automate) the person's file, as overrides of what differs from the team. A teammate who pulls a configured project is
  asked nothing.
- **Guard:** `vcs.OpMerge` is gated by `permissions.merge` like the other operations (a missing key reads as `ask`).
  The merge itself is not implemented yet (the platform operations come next).
- **Doctor:** reports whether delivery is configured (pointing at `gabitotl vcs setup`) and warns, never fails, when the
  platform of `origin` differs from `vcs.md`.

## D34. Hosts: GitHub and GitLab through their CLI, Bitbucket Cloud through REST, and a login helper
- **Problem:** only GitHub could open a PR, nothing could merge, retarget or read checks, GitLab was missing,
  Bitbucket was a stub, and nothing told a person that they were not logged in until a command failed.
- **Decision:** `vcs.Host` extends `PRProvider` with `MergePR`, `SetBase`, `ChecksState` and `AuthStatus`.
  `GitHub` drives `gh`, `GitLab` drives `glab` (both through the injectable `Runner`; the CLI owns the
  credentials) and `Bitbucket` calls the Bitbucket Cloud REST API 2.0 with an injectable `http.Client` and base
  URL. `HostFor` builds the host of a `vcs.md` provider and fails with `ErrUnsupported` for anything else.
  `CreatePR` is idempotent: an open PR of the head branch is returned instead of opening a second one.
- **Merge:** methods are `squash`, `merge` and `rebase` and an unknown one is refused before any call. GitLab has
  no flag for a plain merge, so `merge` passes none; Bitbucket maps them to `squash`, `merge_commit` and
  `fast_forward`. Merging only runs through `Repo.MergePR`, which applies `Gate(OpMerge)`; retargeting a stacked
  PR (`Repo.RetargetPR`) is gated by `pull_requests`. Nothing force-pushes and titles and bodies go through the
  attribution guard before any request.
- **Auth:** `AuthStatus` returns logged-in, not-logged-in, tool-missing or unsupported, always with a `Fix` that
  holds the exact command. Bitbucket has no official CLI, so the per-person `auth` reference (`env:NAME`) names
  an environment variable; the value is looked up in the process environment and then in the 0600 secrets file
  that team memory already uses (`mcp.Resolver`, `mcp.WriteClientSecret`, unchanged). A plain token is a Bearer
  token and `user:app-password` is Basic. A `keychain:` reference is refused for Bitbucket. The token is never
  logged, printed or echoed in an error (errors carry the method, path, status and Bitbucket's own message).
- **Not supported:** Bitbucket Server and Data Center (any host other than `bitbucket.org`) report `unsupported`.
- **Commands:** `gabitotl vcs login [--token-env NAME] [--store-token] [--check]`. GitHub and GitLab run the
  tool's own `auth login` attached to the terminal, only when there is one; without a terminal the command
  prints the fix and exits non-zero, so an agent never hangs on a prompt. `vcs status` has an `auth:` line (the
  reference moved to `auth_ref:`), and `doctor` reports the login as a warning with the fix; for Bitbucket it
  only looks for the token offline.
- **Setup:** with no provider given and the configured one empty or the generic `git`, `vcs setup` adopts the
  platform `origin` points at (a real run left a GitLab project as `git`). An explicit choice is never touched,
  and a bare `--yes` still writes nothing.
- **Tests:** every host runs against a fake `Runner` or an `httptest` server; the CLI tests replace `gh`, `glab`,
  the terminal and the Bitbucket API through hooks (`vcsRunnerFn`, `vcsTTYFn`, `vcsAPIFn`) and the test binary
  defaults to a machine without any of them.
- **Open points:** the exact JSON of `gh pr checks --json` and `glab mr view --output json`, and Bitbucket's
  `fast_forward` strategy for "rebase", were written from the tools' documentation and not exercised against a
  real account. The delivery commands (`commit|push|pr|merge|ship`) that use all this are the next step (S3b).


## D35. Delivery commands: commit, push, PR and merge follow the saved settings
- **Problem:** the settings (D33) and the hosts (D34) existed, but nothing ran commit, push, PR and merge from them,
  so every agent had to improvise the sequence and could stop to ask questions the person had already answered.
- **Decision:** `gabitotl vcs commit|push|pr|merge` are the agent-facing entry points. They never prompt once
  the project is configured. The only question that exists is the `ask` permission of the operation being run,
  and only on a terminal (a single y/n, in Spanish); without a terminal the command stops and names the setting
  and `gabitotl vcs setup`. A missing login is never a prompt: the exact fix is printed and the exit is non-zero.
- **Commit:** `-m` is required and must be a Conventional Commit without attribution (the authorship guard still
  strips or refuses first). With no `--paths` everything is staged with `git add -A`, then `.gabitotl/local/` is
  unstaged (naming an ignored directory in an exclude pathspec makes git fail, so the exclusion is applied
  afterwards); explicit paths inside it are refused. Nothing is committed on the base branch.
- **Base branch:** `base_branch` from `vcs.md`, else what `origin/HEAD` points at, else `main` or `master` when one
  exists, else an explicit error asking for the setting. Push refuses it by name; a PR cannot start from it.
- **Merge:** waits for the checks (default 20 minutes, polled every 15 seconds through an injectable clock). `passing`
  and `none` merge with `merge_method` and `delete_branch`; `failing` never merges and `pending` after the wait never
  merges, both exit non-zero. The permission is consulted before the wait (a `deny` costs nothing, an `ask` is
  answered once), and a PR with no checks merges at once, so a repository whose CI starts late can merge before
  its checks register. Without `--pr` the PR is looked up with the optional `PRFinder` of the host.
- **After a merge:** fetch with prune; when HEAD is on the merged branch, switch to the base and `pull --ff-only`;
  delete the local branch only when `delete_branch` is on and its tip equals `origin/<branch>` as it was before the
  merge (`-D` is needed because a squash leaves the branch looking unmerged). Anything unsafe is skipped with a note
  and never fails a merge that already happened.
- **`vcs ship` (implemented, `ship.go`):** chains of stacked PRs are merged with the plain `merge` method whatever
  `merge_method` says, because squashing a parent rewrites the commits its children are built on and force-pushing
  to repair them is forbidden; PR k+1 is retargeted to the base before PR k is merged. `ship` never pushes the base
  branch: from the base it first creates a task branch at HEAD and leaves the local base alone. Size is never a
  question: work above `pr_budget_lines` is sliced at commit boundaries according to `chain`.
- **Slicing details:** authored lines are additions plus deletions from `git show --numstat` of each commit ahead of
  `origin/<base>` (binary files and a small list of lock, snapshot, minified, generated and vendored paths are not
  counted). The split is greedy at commit boundaries; a commit over the budget is its own slice, reported and never
  split. Slice branches `<branch>-N` are created at each slice end and published; the original branch is left alone.
  The merge question is asked once for the whole series. For each part the checks are awaited, the next part is
  retargeted to the base, and only then the part is merged; a failure midway stops there and reports what merged.
  A merge stopped by `deny`, by `ask` without a terminal or by a declined `ask` is a planned outcome and exits 0
  with the reason in the result (`merge-denied`, `merge-ask`, `merge-declined`).
- **Tests:** temp repositories with a bare repository on disk as `origin`, a fake `Host` and a fake clock; nothing
  touches a network, a real `gh`/`glab` or this repository. The authorship-guard hooks that `init` installs call the
  `gabitotl` binary, which under test is the test binary itself, so tests that commit after `init` point
  `core.hooksPath` at an empty directory.

## D36. The eight skills are GabitoTL's own; they do not copy SDD phases
- **Problem:** the persona is always on and must stay small, but delegation, memory, team and delivery rules are
  too long to repeat in it. Importing another harness's phase skills would also bring its workflow and vocabulary.
- **Decision:** eight embedded skills (`gabito-orquestador`, `-recolector`, `-escritor`, `-verificador`, `-memoria`,
  `-equipo`, `-entrega`, `-terminal`), each loaded only when its moment comes. They describe roles (orchestrator,
  one sequential helper) and routes (inline, mapper, writer, verifier) instead of SDD phases, so a small task costs
  nothing extra. `gabito-entrega` replaces work-unit-commits, chained-pr and branch-pr and drives the `vcs`
  commands of D33-D35. The persona gets one line telling the agent to load the matching skill.
- **Drift test:** a test in `internal/cli` extracts every `gabitotl <command> [<subcommand>]` from the skills'
  backticks and code blocks and fails if the command or subcommand is not in the CLI registry; another keeps the
  subcommand lists honest against each group's usage, and a third forbids any "size exception" wording.
- **Memory tools:** the skills name the `memory_*` bridge tools, which serve Engram and team projects alike.

## D37. The delivery setup can run as a conversation with the agent

- **Decision:** `gabitotl vcs setup` stays the one writer of the delivery settings, but the questions no longer
  need a terminal. `vcs setup --questions` prints, as JSON and read-only, the questions that apply to this project
  and person (`scope` team or personal), each with its title, options, default, answer flag and the automation
  answers it depends on. The `gabito-entrega` skill makes the agent ask them in chat, one at a time, and save the
  answers with a single `vcs setup --yes --vcs-...` call.
- **Why:** the user wants the orchestrator to hold the dialogue (inside Claude Code, Kiro or OpenCode) instead of
  sending them to a fixed command; an agent's shell has no terminal, so a prompt there never works.
- **Single source:** the JSON is built from the same `NextDeliveryStep`/`DeliveryQuestion` data as the plain
  prompt and the TUI, so the three never diverge. Nothing about PR size, budgets or exceptions is ever listed.

## D38. Own chat-concision modes instead of a third-party plugin

- **Decision:** `gabitotl persona mode [normal|lite|full]` sets how short Gabito's chat replies are. The default
  is `normal`; `lite` drops filler and closing offers; `full` is telegraphic. With no argument the command prints
  the active mode and its rules, and the persona carries one line telling the agent to run it at the start of a
  chat and whenever the user asks for shorter or longer replies.
- **Why:** a terse-reply plugin was evaluated and rejected. Gabito is already terse, so the gain is small; its
  style clashes with the es-AR voice; it can degrade PR, memory and doc text; and it would eat the persona
  budget (the persona sits at about 1500 estimated tokens).
- **Consequences:** the mode is per person, never shared: `.gabitotl/local/users/<id>/persona.md` (git-ignored)
  in a project, else `~/.gabitotl/persona.md`, read in that order and falling back to `normal` (an unreadable or
  invalid file never fails a command). The rules text lives in the binary and is printed by the command, not
  installed in the persona. The rules apply to chat only: artifacts (code, commits, PRs, docs, memory), security
  warnings, destructive-action confirmations and questions to the user are never shortened.

## D39. Context budget is enforced by host hooks

- **Decision:** `gabitotl context add|report|check|checkpoint` wires `internal/budget`, and the Claude Code adapter
  installs two owned `settings.json` hooks: `PreCompact` runs `gabitotl context checkpoint`, and `UserPromptSubmit`
  runs `gabitotl context hook`, which measures the transcript (the latest assistant `usage`, else file size / 4) and,
  once per crossing of the threshold, injects a one-line instruction to checkpoint. Runtime text is estimated at
  chars/3.6 (`EstimateTextTokens`); the persona-size check keeps chars/4.
- **Why:** the 75% rule existed only as prose that a long chat forgets, and nothing called the budget code. Hooks run
  outside the model, cost no tool call, and read real usage.
- **Consequences:** hooks are declared by the adapter through the ownership engine (backed up, removed on uninstall);
  core never branches on runtime names (D3). `Stop` is unused because its output cannot add context. An existing
  user hook for the same event is preserved and ours is skipped. OpenCode and Kiro have no equivalent, so they keep
  the prose rule. The check is re-armed when usage drops under the threshold (after compaction). Hook failures never
  block the host: every error exits 0 silently.

## D40. Memory reaches the agent as a capped bundle

- **Decision:** `gabitotl memory context --task "..."` runs `BuildQueries` + `Retrieve` and packs the entries into a
  compact bundle: one header line each (type, title, id) plus a body cut at a sentence or word boundary to
  `--per-entry-tokens` (250), stopping at `--max-tokens` (1500), then a `memory: N entries, ~T tokens (K omitted)` line.
  `memory search` gains `--json` and rejects an out-of-range `--limit` (exit 2) instead of silently resetting it.
- **Why:** retrieval capped the entry count but never the bytes, and nothing outside tests called it, so the agent
  pulled whole entries by hand. The persona points at the command (same size); the memory skill carries the detail.
- **Consequences:** packing lives in `internal/memory` and repeats the chars/3.6 estimate because `budget` imports
  `memory`. With no provider the command prints one notice and exits 0. Omitted entries are counted, not listed;
  `memory search` still reads one in full.

## D41. History scan is incremental

- **Decision:** `guard scan --history` remembers, per person, the ref tips it scanned, the findings in that history and
  a fingerprint of the rules (`historyRulesVersion` + sorted extra terms) in `.gabitotl/local/users/<id>/guard-history.json`.
  Next time it runs `git log --all --not <tips>` (at most 500 tips) and reports cached plus new findings. `--full`
  forces the old full read.
- **Why:** the scan read the full body of every commit on every ref each time. History is never rewritten (D10), so an
  old finding must not defeat the cache.
- **Consequences:** a rules change, an unreadable cache or a git failure (a tip object gone) means one full scan.
  Bump `historyRulesVersion` whenever branding detection changes. `guard fix` and `doctor` still scan fully.

## D42. Per-person hooks and a lighter persona

- **Decision:** Claude Code hooks (D39 plus `SessionStart` → `gabitotl persona mode --hook`, which injects the active
  chat mode as `additionalContext`) are written to `.claude/settings.local.json` at project scope and to
  `~/.claude/settings.json` at user scope; attribution keys stay in the shared file. Owned hooks an older version put
  in the shared `.claude/settings.json` are removed on the next apply. The terminal, noisy-terminal and remote
  sections left the always-on persona for the `gabito-terminal` skill (persona keeps one line to load it and the
  one-line remote-authorization rule); the helper rule now reads "one helper at a time, sequentially".
- **Why:** project hooks in the shared file break teammates without gabitotl, the mode cost a tool call per chat, and
  the persona sat at the 1500-token cap (about 1500 to 1200 estimated tokens).
- **Consequences:** `normal` injects a one-line confirmation, not nothing, so the fallback tool call is not triggered.
  OpenCode and Kiro keep the fallback ("if no persona-mode rules were injected, run `gabitotl persona mode`"). The
  installer does not edit `.gitignore`; Claude Code ignores `settings.local.json` itself. Fixing a JSON splice bug in
  `ownership.Set` was needed to add a second key after a pretty-printed one.

## D43. Noisy commands reach the agent as a digest

- **Decision:** `gabitotl run -- <cmd>` runs the command without a shell, saves the full combined output to
  `.gabitotl/tmp/run-<time>.log` (OS temp dir outside a project) and prints a header plus failure lines with two lines of
  context, deduplicated and capped, and the last lines. The exit code is the command's (127 if not found). The logic
  lives in the pure `internal/digest` package. Bytes saved go to the append-only `local/users/<id>/savings.log`.
- **Why:** test, build and lint output dominates context use; the agent needs failures and the verdict, not the stream.
- **Consequences:** failure detection is heuristic (keywords and `file:line:col:` positions), so the log is always
  kept and the `gabito-terminal` skill tells the agent to search it, not read it whole. Statistics are best-effort and
  never fail a run. A separate log file was chosen over `session.md` to avoid read-modify-write races.

## D44. The project map follows git

- **Decision:** inside a Git work tree the scanner lists files with `git ls-files -z --cached --others --exclude-standard`
  (so `.gitignore` is honoured and a monorepo needs no directory walk); without git it falls back to the old walk and
  skip list. Past the file cap (5000) the rest is only counted per top-level directory and type, and the map says it
  was truncated. `gabitotl scan` keeps the map in `.gabitotl/local/users/<id>/scan-cache.json` with the HEAD it was
  built from and a hash of the untracked-file list; it is reused while both match, `--refresh` rebuilds. The rendered
  map is cut to about 800 tokens by shortening lists and says so. `DetectSignals` uses the same listing.
- **Why:** the walk ignored `.gitignore`, stopped silently at 5000 files and re-read everything on every call.
- **Consequences:** the map is an aggregate, so a changed HEAD or untracked set means a full rebuild (cheap with
  `ls-files`), not a per-file patch. Uncommitted edits to tracked files do not invalidate the cache. No cache is written
  before `init` or without an active identity.

## D45. Code index is optional, CodeGraph first

- **Decision:** `internal/codeindex` detects two backends, CodeGraph (default) and code-review-graph, by an executable
  on PATH plus an index directory in the project (`.codegraph/`, `.code-review-graph/`). `gabitotl index status`
  reports them, `index setup` prints (or with `--yes` runs, as an argument list, no shell) the tool's own
  `codegraph init --yes <root>` / `code-review-graph build`. `doctor` adds one informational line. The recolector skill
  tells the agent to use an available index before reading files and to check `index status` once per session.
- **Why:** an index answers structure questions in one call instead of many reads. One MCP tool (CodeGraph) costs far
  less context than ~30. polycodegraph was rejected as a third, overlapping graph.
- **Consequences:** nothing requires an index and GabitoTL never installs one. The setup argv does not depend on the
  runtime, so core has no runtime branching (D3).

## D46. Memory compaction folds only operational entries

- **Decision:** `gabitotl memory compact` (dry run by default) folds only `progress` entries without a topic key that
  are older than `--older-than` (default 30d) into one "Progress digest YYYY-MM" per project and month, and drops exact
  duplicates (same title and content, newest kept). Knowledge types and topic-keyed entries are never touched. `--apply`
  confirms (or `--yes`), writes a JSON backup under `.gabitotl/.bak/`, saves the digests, then soft-deletes the originals.
- **Why:** old progress notes add noise to searches, but decisions and fixes are the history worth keeping. Soft delete
  plus the backup means nothing is silently lost (D10).
- **Consequences:** only the team server store lists and deletes, so the Engram provider is refused instead of
  half-applied. Digests carry the topic key `digest/progress/YYYY-MM`, so a re-run is idempotent. Session summaries
  kept in their own table are not touched.

## D47. Savings are measured, not claimed

- **Decision:** there is one token estimator (`internal/tokens`: `Estimate` at ~3.6 characters per token, `EstimateLegacy`
  at chars/4 for the persona budget). `gabitotl stats` reports savings from `savings.log` (written by `run` and
  `memory context`), the session context and the always-on cost. Docs describe how to measure, never a fixed percentage.
- **Why:** a saving nobody can reproduce is marketing. The estimator was copied in three packages and could drift.
- **Consequences:** numbers are estimates and say so; a bytes-saved figure ignores what the agent would not have read
  anyway. Per person and local, so there is no team-wide total.

## D48. Kiro hooks are per-person, plain text and non-blocking

- **Decision:** the Kiro adapter owns one file, `<KIRO_HOME or ~/.kiro>/hooks/gabitotl.json`, for every scope (never the
  shared project `.kiro/`). It holds two `SessionStart` hooks: `persona mode --hook --format text` and a silent
  `context hook --format text --event session --dump` that captures the undocumented Kiro stdin. No `PromptSubmit` hook
  until a dump shows usage fields. Hook commands accept `--format claude|text` (default `claude`), always exit 0, never
  write to stderr and print nothing when idle, because Kiro adds every stdout to the agent context. Hook commands for Kiro
  and Claude Code use the absolute path of the installing binary (`Env.Exe`, quoted when it has spaces; plain
  `gabitotl` under `go run`/temp dirs), and a re-install replaces our earlier entry.
- **Why:** a project-level file breaks teammates without gabitotl; a failing `PromptSubmit` can block prompts in the IDE and
  GUI apps may not have gabitotl on PATH.
- **Consequences:** no pre-compaction event, so the 75% checkpoint stays a persona rule. IDE loading of `~/.kiro/hooks` is
  unconfirmed. A moved binary needs a re-run of `install`.

## D49. Gabito leads the technical vision and explains with everyday examples

- **Decision:** the persona has a "Lead with your expertise" section: the user owns *what*, Gabito owns *how*. Gabito
  reviews, challenges and proposes improvements, risks and fixes with its professional opinion; when advising it may leave
  the block format and talk it through, always explaining with an everyday example and without code detail unless asked.
  Chat-concision modes keep full sentences for these proposals.
- **Why:** many users know the outcome they need but not how to code it; short structured replies alone hid the expert
  judgment they rely on.
- **Consequences:** advisory replies can be longer than the block format; persona grew to ~1346 of 1500 tokens. Big
  unrequested changes are still proposed, never made.

## D50. Reuse before creating; rollback point before modifying

- **Decision:** the persona and the `gabito-escritor` skill require searching for existing functions, modules and
  components first and adapting, extending or modularizing them instead of creating a parallel version; a new file needs a
  stated reason. Before modifying existing code there is always a rollback point: a clean Git state (commit or stash
  pending work first) plus `gabitotl backup create` for files Git does not track cleanly.
- **Why:** duplicated modules multiply files and drift apart; changes without a rollback point turn a mistake into lost
  work.
- **Consequences:** persona at ~1408 of 1500 tokens; little room is left for new always-on rules, so future rules should
  live in skills.

## D51. Push by milestone on GitHub and GitLab

- **Decision:** on GitHub and GitLab the `gabito-entrega` skill works with local commits and publishes only at an
  important milestone, asking the user first even when the saved automation is `all`. Checks run locally instead of
  pushing to see if CI passes, and fixes are pushed in one batch. Bitbucket and other hosts follow the saved settings.
  CI tuning (`concurrency`/`cancel-in-progress`, `paths-ignore`, light draft PRs) is proposed, never applied unasked.
- **Why:** every push, PR update and merge can spend paid Actions or CI/CD minutes; local commits already give the
  rollback point.
- **Consequences:** the rule lives in the skill, not the persona (no budget left there), and is behavioral: the CLI
  still pushes when told to, so `vcs ship` keeps its no-prompt contract.

## D52. Kiro context is measured from Kiro's own usage percentage

- **Decision:** Kiro writes `contextUsage` records (`usagePercentage`) into `<KIRO_HOME or ~/.kiro>/sessions/*/sess_*/messages.jsonl`.
  `context check|report|hook --source kiro` finds the newest session whose `workspacePaths` or `rootPaths` contain the
  project root, reads the last such record (the last 256 KB first, the whole file only as a fallback, malformed lines
  skipped) and stores it as a share of the limit (`budget.SetPercent`), so the existing threshold, one-shot warning and
  re-arm after a drop work unchanged. The Kiro adapter installs a third hook, `PromptSubmit`
  (`context hook --format text --source kiro --event prompt`, timeout 5), which always exits 0 and prints nothing under
  the threshold. Past it, it prints the usual checkpoint instruction and TIP once per crossing.
- **Why:** Kiro auto-summarizes at about 80-88%, so the warning must come before that, and Kiro does measure usage itself.
  The source is a flag value, not a runtime name in core (D3).
- **Consequences:** this reverses the D48 removal of `PromptSubmit`, now justified because the hook cannot fail or print
  noise. The session format is undocumented and may change; an unreadable one just means no warning. OJO: deleting the
  gabitotl binary without `gabitotl uninstall` leaves a hook pointing at a missing file, and Kiro may then show an error
  on every prompt. Real IDE stdin and trigger behavior are unverified; the hook relies on cwd or the working directory.

## D53. Apache License 2.0

- **Decision:** GabitoTL moves from MIT to the Apache License 2.0, with a `NOTICE` file; every shipped skill declares
  `license: Apache-2.0`.
- **Why:** it stays permissive for people and companies, but adds an explicit patent grant and requires keeping the
  copyright and notice, so authorship travels with every copy.
- **Consequences:** redistributions must keep `LICENSE` and `NOTICE`; the skills test enforces the new license field.

## D54. The persona language is chosen at install (es | en)

- **Decision:** Gabito's chat voice has two languages, `es` (Argentine Spanish, the default and the exact previous
  behavior) and `en` (plain English). There is still one persona template: the voice lines (`voice.reply`,
  `voice.example`), the block labels, the name of the heads-up concept (`term.ojo`) and the other replaceable messages
  come from a per-language messages file selected by `persona.Lang`. English labels: *What I did / Progress / Next /
  Heads-up / TIP*. The installer asks the language once (TUI form and plain prompt) and `install --persona-lang es|en`
  answers it without a terminal. The choice is saved as `lang` in `~/.gabitotl/persona.md` after a successful run.
- **Why:** English speakers could not use the harness comfortably, and a parallel English persona would have forked every
  rule (D50 asks for one persona text). The language belongs to the person and to the global persona file the
  runtimes load, so it lives in the personal home file, not in the committed project files.
- **Consequences:** changing the language is a reinstall with the flag, and the owned-section engine replaces the persona
  in every runtime. Both texts stay under the 1500-token budget and are tested for the same required rules. Reinstall and
  upgrade keep the saved choice; `doctor` shows it and verifies the installed persona against it. The CLI's own screens,
  the guard messages and the `gabito-*` skills remain Spanish (OJO).

## D55. Visual Windows setup and Manager (Wails, per-user self-install)

- **Context:** Windows users had to install Go or unpack a release by hand, edit PATH and run a terminal installer.
  They asked for a visual, double-click setup, and for the same app to manage GabitoTL afterwards.
- **Decision:** one downloaded executable, `GabitoTL-Setup-<version>-windows-amd64.exe`, is both setup and Manager.
  It is a Wails v2 app (WebView2) with a plain HTML/CSS/JS frontend and no npm step, kept in `cmd/gabitotl-manager`
  under a `windows` build constraint. All logic lives in headless packages: `internal/gui` (the Manager backend,
  reusing the CLI's installer, doctor and backup code per D50) and `internal/desktop` (self-install through ports:
  copy, user PATH, shortcuts, Apps & features). The install is per user under `%LOCALAPPDATA%` and never needs admin
  rights. Every change is a reviewed plan first. The CLI is embedded in the setup (`-tags embedcli`) and written next
  to the manager so PATH and the guard hooks work from a single download. Version comes from the same
  `internal/version.Version` ldflag as the CLI; `make manager-windows` builds it. CI builds and tests the
  Windows-relevant packages on `windows-latest`.
- **Why:** it keeps one implementation of install logic, keeps Linux and macOS builds free of Wails and CGO, and makes
  the download self-contained. Per-user avoids UAC and keeps uninstall simple.
- **Consequences:** the setup is about 20 MB larger because it carries the CLI. Windows behavior is verified only by
  unit tests with fakes and a CI build; the GUI has not been run on a real Windows machine (OJO). Only the Windows-
  relevant packages run in the Windows CI job; the full suite and e2e stay on Linux and macOS. Code signing is not set
  up, so SmartScreen may warn on first run.
- **Running copies:** install, upgrade and uninstall first find running copies of the owned executables (image path
  inside the install dir, same user only, never the current process) and close them: WM_CLOSE to their windows, a
  short grace period, then TerminateProcess. If one cannot be closed, an install stops before copying anything. The
  detached cleanup waits for the Manager to exit, retries up to about a minute, and also removes the WebView2 profiles
  and the log under `%LOCALAPPDATA%\GabitoTL` (only those named folders), then the install and data folders if empty;
  leftovers are listed in `%TEMP%\gabitotl-uninstall.log`. This closes the "in-place upgrade of a running exe" gap
  (still unverified on real Windows).

## D56. Credentials in the OS credential store; global delivery defaults layered under the project

- **Context:** the install is global and must work with no project, yet each project may live on a different Git
  platform. The first Windows test showed the sign-in asked for a project and an "env var name" field confused users.
- **Decision:** (1) Tokens live behind a port, `internal/credstore.Store` (`Get/Set/Delete` keyed by platform host).
  Adapters: Windows Credential Manager through advapi32 (`CredWriteW/CredReadW/CredDeleteW/CredFree` via
  `golang.org/x/sys/windows`, generic credential, target `gabitotl:vcs:<host>`, `CRED_PERSIST_LOCAL_MACHINE`, user
  `gabitotl`, no CGO); on other systems a 0600 file `~/.gabitotl/secrets/vcs-credentials` in a 0700 directory,
  written atomically; and an in-memory fake for tests. (2) Delivery defaults live in `~/.gabitotl/vcs.md`, parsed by
  the same code as the project file. Load order: built-in default, global file, project team `vcs.md`, per-person
  file. (3) Token lookup for the platform clients: process environment and `env:NAME` reference first, then the
  credential store by host. (4) `vcs.VerifyToken` checks a token against the platform API with no project; the
  Manager verifies before it stores.
- **Why:** per-user OS storage (DPAPI) is safer than a file and needs no project; reusing the existing parser keeps
  one implementation of the delivery settings; env-first keeps CI and existing setups working unchanged.
- **Consequences:** `vcs login --store-token` now stores into the credential store (not the memory secrets file).
  Tokens never appear in any `.md` file, log or Manager return value. GitHub and GitLab operations still go through
  `gh` / `glab`, which do not read this store yet (resolved: `vcs.HostFor` hands the stored token of the remote's host to `gh` / `glab` in that child's environment only, `GH_TOKEN` / `GH_ENTERPRISE_TOKEN`+`GH_HOST` / `GITLAB_TOKEN`+`GITLAB_HOST`, and never when the person already set one);
  custom hosts can be stored and verified but the Manager lists only the three public hosts. The Windows adapter is
  compile-checked only and has not run on a real Windows machine.

## D57. CodeGraph is a required, pinned component installed by the Windows setup

- **Context:** the user made CodeGraph mandatory for the Windows setup. The official `install.ps1` downloads whatever
  is latest and `codegraph install` rewrites `CLAUDE.md` / `AGENTS.md`, which GabitoTL manages (D50).
- **Decision:** (1) `internal/codegraph` pins release v1.6.2 and the SHA-256 of `codegraph-win32-x64.zip`
  (`7455e817ee80...deea`) and `codegraph-win32-arm64.zip` (`308f7bf92e78...c071`), taken from the release's
  `SHA256SUMS`. A mismatch fails closed: nothing is extracted or activated. The zip is self-contained (bundled
  `node.exe`), so no Node/npm. (2) Same layout as the official installer (`%LOCALAPPDATA%\codegraph\current`,
  `current\bin` on the user PATH, added once through `internal/desktop.PathEnv`) so `codegraph upgrade` keeps working;
  a replaced install renames locked files aside (`*.old-<token>`). An install at the pinned version or newer is left
  alone. (3) The MCP server `codegraph serve --mcp` is registered by GabitoTL's own runtime adapters (new
  `rt.Options.Tools`, planned like the memory entries and backed up by `BackupAndApply`), never by `codegraph install`.
  On Windows the entry is `cmd /c codegraph serve --mcp` because the launcher is a `.cmd`; the PATH entry keeps it valid
  across upgrades. (4) The wizard installs it before configuring any agent and stops if it fails; the Manager exposes
  `CodeGraph`, `PlanCodeGraph` and `ApplyCodeGraph` (plan-first) to repair or upgrade.
- **Why:** a pinned, verified artifact is reproducible and tamper-evident; reusing the runtime adapters keeps one config
  writer and one backup path.
- **Consequences:** bumping CodeGraph means changing the version and hashes in one file. Linux and macOS are not wired
  (npm remains the hint). Unverified on a real Windows machine; `cmd /c` as the stdio command is the common Windows
  pattern but has not been exercised with each agent.
- **Hardening:** the store key is the host name without a port (a token for `git.corp:8443` serves any remote on `git.corp`); an empty or relative home is refused, never a path under the working directory; tokens over 2560 bytes are refused on Windows; token checks never follow redirects; error text is scrubbed of the raw, query- and path-escaped token. Known and left: a 403 from the platform counts as rejected, and the remote is looked up on every `gh`/`glab` call.

## D58. Project discovery is read-only; SQLite lives only in the Manager binary
- **Decision:** `internal/projects` defines a `Source` port and a merger (`Scan`). Sources: Claude Code (`~/.claude.json` `projects` keys), OpenCode (`opencode.db` table `project`, legacy `storage/project/*.json`), Kiro (`workspaceStorage/*/workspace.json`, `globalStorage/storage.json`, `file://` URIs), the folders the Manager remembers (`manager.json`, a list beside the old single `root`) and a bounded scan (depth 3, a few usual roots) for `.gabitotl/`. Paths are normalised (Windows: case-insensitive key, drive letter upper-cased), missing folders, the home folder and filesystem roots are dropped, duplicates merge their sources, and a deadline and a maximum bound the work. Every source is best effort and silent on absence or parse errors.
- **SQLite:** OpenCode's database is read through `modernc.org/sqlite` (pure Go, CGO-free) opened `mode=ro` with `query_only`, in the separate package `internal/projects/opencodedb`, imported only by `cmd/gabitotl-manager`. The Linux/CLI binary does not link it (`go list -deps ./cmd/gabitotl` shows no sqlite).
- **Why:** OpenCode may be running, so nothing is written or locked beyond a read; keeping the driver out of the CLI keeps it small.
- **Consequences:** the Manager binary grows by about 4 MB. Kiro's layout is low confidence (fixture-tested only); the Windows paths of OpenCode and Kiro are unverified on a real machine. `gui.Service` exposes `Projects`, `AddProject`, `ForgetProject`.

## D59. The Manager has two levels: Global and Projects
- **Decision:** the install is global, so the Manager separates what is global (delivery defaults, credentials, CodeGraph, agents, persona, Windows, backups, uninstall) from what belongs to a project folder (delivery override, code index, guard hooks, memory, doctor, project-scope agents, backups). Selecting a project from the discovered list (D58) opens its page and remembers it; screens receive the open project as their root, the global ones always use none.
- **Backend:** `gui.Service.Project` (git and platform from the remote, only the host is kept; effective delivery with its source `project | global | default`; whether the host's token is stored; code index and guard state), `ResetProjectDelivery` (`gabitotl.ResetDeliveryToGlobal`), `PlanProjectSetup`/`ApplyProjectSetup` (the CLI's own `gabitotl.Init`, then the new `vcs.md` takes the global defaults) and `IndexProject` (progress, cancellable through the context). No second init implementation.
- **Provenance rule:** a project is "project" when its `vcs.md` is marked `configured` or the person has permission overrides; otherwise it follows the global layer. A `vcs.md` written by an older `gabitotl init` carries built-in values that mask the global ones (the layering is key by key); use "Use the global defaults" to realign it.
- **Consequences:** the persona chat mode is saved globally from the Manager (the per-project, per-person mode stays CLI only); backups are shared between the levels because snapshots live under `~/.gabitotl`.

- **Hardening:** every project-scoped Service entry point first runs `checkRoot` (non-empty absolute path to an existing directory, `gabitotl.ValidateRoot`, which now also refuses Windows drive roots and UNC share roots through the pure `gabitotl.IsFilesystemRoot`); setup never creates the folder. `IndexProject` is single-flight (`gui.error.busy`) and the app clears its cancel function only when it still is the one of its own operation. The page drops results of a superseded load (sequence token).
- **Known limit:** the delivery and guard-hook plans of the Manager are built client-side (a summary of what the person chose) rather than by a backend plan; the writes themselves validate on the Go side.

## D60. Bitbucket Data Center through a custom-host registry; TLS uses the OS trust store
- **Decision:** a new provider value `bitbucket-server` (Bitbucket Server / Data Center, REST API 1.0: `/rest/api/latest` pull requests, `/rest/build-status/latest` checks, `/rest/branch-utils/latest` branch delete) next to `bitbucket` (Cloud). Authentication is an HTTP access token sent as `Authorization: Bearer`. Self-hosted servers are a non-secret registry `~/.gabitotl/vcs-hosts.json` (`internal/vcshosts`: port `Registry`, atomic JSON file adapter, fake) with `host` (the credential-store key, no port), `platform` (`bitbucket-server`, `github`, `gitlab`) and the web `url` (scheme, port, optional context path; no userinfo, query or fragment). A remote whose host is registered takes the registered platform (`vcs.InferProviderWith`); the API base URL always comes from the registry because an SSH port (`ssh://git@host:7999/...`) says nothing about the HTTP one.
- **Behaviour:** merge reads the pull request `version` first (a stale one is a 409) and maps squash / merge / rebase to `squash` / `no-ff` / `rebase-no-ff`; deleting the source branch uses branch-utils after the merge; retargeting is a versioned PUT; checks come from the build statuses of the head commit; the auth check calls an authenticated-only endpoint and requires `X-AUSERNAME` (anonymous access does not prove the token). `vcs.VerifyToken` takes the registry URL (`VerifyOptions.BaseURL`); `vcs login --url`, `vcs test` and the Manager's `SaveServer`/`TestCredential` verify before anything is stored. No redirect is followed with a token.
- **TLS:** the default Go client, therefore the Windows certificate store and the Linux system bundle (`SSL_CERT_FILE`). There is no insecure-skip option; a certificate problem is reported as "certificate not trusted by this system" so a missing corporate CA is recognisable.
- **Why:** corporate and personal projects on different hosts need per-host tokens and a place that says where each host's API lives, without putting a secret or an address guess in `vcs.md`.
- **Consequences:** `draft` is sent but ignored by servers older than 8.18; a merge strategy the server does not enable fails with the server's message. Endpoints follow Atlassian's Data Center REST reference (fetched through Context7); nothing was run against a real server.
- **Hardening (round 4 fixes):** (1) A registered URL must be `https://`; plain `http://` is accepted only for loopback (`localhost`, 127.0.0.0/8, `::1`). `vcs login --allow-insecure-http` overrides it for a trusted network and warns that the token travels in clear text; the Manager never overrides and answers `insecure_http`. (2) The project key and repository slug read from a remote become API path segments, so they must match `^~?[A-Za-z0-9][A-Za-z0-9._-]*$` (never `.` or `..`). (3) **One token per host name:** the registry key and the credential key are the host without the port, so a host name holds exactly one server. Registering a different URL (port, scheme, context path) or platform under a known host name, or an SSH alias that belongs to another entry, is refused with `ErrHostConflict` unless replaced explicitly (`--replace`; the Manager shows a "Replace" button and asks for the token again, and never reuses the old server's token). (4) Only HTTP 401 means "not logged in"; a 403 is a permission problem (`ErrNoPermission`, with the server's scrubbed message). (5) A server may have Git remotes on a different host than its web address: an entry may list `sshHosts` aliases (`--ssh-host`, repeatable; a field in the Manager). They only identify a remote; the token is read by the entry's web host and sent only to its URL. (6) The registry file is re-read right before each write and fsynced before the rename; a corrupt or unreadable file is an error naming the path, never "not registered". (7) Failures carry a stable `code` (`cert_untrusted`, `rejected`, `no_permission`, `unreachable`, `timeout`, `insecure_http`, `host_conflict`, `invalid_url`, `not_stored`, `unknown_host`) set from typed errors; the Manager words them from `gui.cred.err.<code>` in the language on screen and falls back to the English detail for an unknown code.
