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

