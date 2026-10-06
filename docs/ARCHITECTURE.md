# GabitoTL architecture

Go module `github.com/winnstorm/gabitotl`. Stdlib first; the only third-party
dependency is `gopkg.in/yaml.v3` (front matter).

## Package layout

| Path | Responsibility |
|------|----------------|
| `cmd/gabitotl` | Thin `main`: calls `cli.Run`. No logic. |
| `internal/cli` | Subcommand dispatcher and commands (`init`, `memory doctor|search`, `scan [--root] [--json]`, `vcs status|branch`, `backup create|list|restore|prune`, `worktree add|list|remove`, `work start|update|status|overlap`, `runtime list|detect|plan|apply|verify|remove|set`, `import detect|run`, `persona print [--stats]`, `version`, `help`; stubs for `install`, `doctor`, `migrate`, `restore`, `uninstall`; the installer TUI is not built yet). Takes an `Env` (stdin/stdout/stderr/cwd/TTY) so commands run in-process in tests. |
| `internal/version` | Build metadata. |
| `internal/gabitotl` | Project layer: `Layout` (all `.gabitotl/` paths), Markdown + YAML front matter codec (`ParseDoc`, `RenderDoc`), typed configs, secret guard, `Init`, `DetectKind`, `FindRoot`. |
| `internal/identity` | Developer identity: slugged IDs, shared profile in `users/`, active identity in `local/user.md`. Never stores emails. |
| `internal/memory` | Provider-agnostic memory contract: `Provider` (Search/Save/Update/SessionSummary/CurrentProject/Doctor), `Entry` with traceability fields, `Guard` (team mode requires `actor`; unknown authorship is `legacy`), `Factory`, relevance query builder (capped, never loads memory in full) and `Recorder` (honors `workflow.memory_after_work`). |
| `internal/memory/engram` | Adapter over the real `engram` CLI behind a `Runner` interface (tests use a fake). Search, Save, Doctor (`engram doctor --json`, read-only), `Projects()`, `Export()`. `Update`, `SessionSummary` and keyless `CurrentProject` have no CLI equivalent and return `ErrUnsupported`; the DB file is never touched directly. Traceability fields travel in a `**Label**: value` content trailer. |
| `internal/memory/mcp` | Abstract Memory MCP + PostgreSQL provider over a thin `Transport` (call a named tool with JSON args). Configurable tool-name mapping; connection by `env:NAME` reference only, resolved at runtime and never persisted. Ships an in-memory `FakeTransport`; no concrete server or PostgreSQL access (server not selected yet). |
| `internal/memory/migrate` | `MemoryMigrator` + `Registry`. No concrete Engram to MCP migrator ships (only migrate with a proven adapter): `Lookup` returns `ErrNoTestedMigrator`. |
| `internal/memory/providers` | Composition root: registers shipped providers in a `memory.Factory`. The only package that imports concrete providers. |
| `internal/scanner` | Layered, bounded project scanner (structure with file cap and skip list, README/docs, manifests to stack and commands, recent `git log`, main modules). Read-only and deterministic; `ProjectMap.ToEntry()` yields the `project-map` memory entry, including open questions for what cannot be inferred. |
| `internal/vcs` | Git layer behind a `Runner` interface (tests use temp repos and fakes, never a remote). Status and provider inference from `origin` (github, bitbucket, gitlab, azure-devops, git, local), task branches, Conventional Commits only (attribution trailers rejected, none added), push, and `Revert` (new commit; the only rollback). Every operation is gated by `VCSConfig.Permissions` (allow/ask/deny). History rewrite (force-push, `reset --hard`, amend, rebase) fails with `ErrHistoryRewrite` before reaching git. PRs go through `PRProvider`: `GitHub` (via `gh`) and a documented `Bitbucket` stub (`ErrNotImplemented`). |
| `internal/backup` | Local snapshots under `.gabitotl/.bak/<user>/<timestamp>_<task>/` with `manifest.md` (user, task, date, branch, base commit, files, reason). Copies only existing files; refuses paths outside the project root (including `..` and symlink escapes); never copies `.gabitotl/.bak` or secret-looking files (`.env*`, keys). `Restore` only restores manifest files and first saves current state in a `pre-restore` snapshot. `Prune` keeps a snapshot if it is among the newest `max_snapshots` OR newer than `max_age_days`. |
| `internal/coordination` | Team support (spec 8). `Worktrees` (over `vcs.Repo`): add/list/remove git worktrees at `<repo-parent>/<repo-name>-worktrees/<task-or-user>` (refuses temp dirs, never forces removal, never touches the main worktree). Session lock `.gabitotl/local/workdir.lock` (pid, user, start; stale-pid aware) returns `BusyError` telling the second user to create a worktree. Task files `.gabitotl/work/<user>/<task>.md` (front matter id/owner/status/branch/areas/files; body Completado/Falta/Próximo paso per spec 29) and `CheckOverlap`, an advisory soft lock over active-work files and memory search (memory failures become warnings; never blocks). |
| `internal/persona` | Output policy and the compact persona. `Classify` (new-project, feature, bugfix, refactor, maintenance, configuration, research; deterministic keyword heuristics, internal only). `Renderer`: small work (1-2 sentences) and long work (mandatory Qué hice / Progreso / Qué sigue; Ojo and TIP only when non-empty), a `Truncate` rule (whole sentences, else word boundary plus an ellipsis) and `ValidateSmall`/`ValidateLong`. `ShouldAsk(Risk)` / `DecideAlone` implement the question policy of spec 19. User-facing wording lives only in `messages_es-AR.txt` (es-AR, replaceable via `ParseMessages`); `persona.md` is the embedded system prompt (about 950 estimated tokens, tested against a 1500 budget) rendered with those messages by `Text`. |
| `internal/budget` | Context-budget monitor. Uses real token counts when the runtime reports them (`ReportTokens`), otherwise `EstimateTokens` (ceil(runes/4)). `Check` returns `Decision{ShouldSave, Message}` once per session when usage reaches the threshold (default 75% of the limit); state in `.gabitotl/local/session.md`. `Checkpoint` saves progress, decisions, next step and the session summary through `memory.Recorder` and returns the es-AR TIP only when memory really recorded it; failures re-arm the trigger and never block the user. |
| `internal/ownership` | Spec 38. Markdown sections wrapped in `<!-- gabitotl:start NAME -->` / `<!-- gabitotl:end NAME -->` with idempotent `UpsertSection`/`RemoveSection` (user content outside the markers is untouched). JSON/JSONC cannot carry markers, so `jsonedit` is a splice editor: it scans the document (comments, trailing commas) and replaces only the managed byte ranges (`Set`, `Remove`, `ArrayAdd/Has/Remove`), never re-serializing. A document it cannot scan, or a scalar where an object is needed, returns `*UnsafeError` (`errors.Is(err, ErrUnsafe)`) and the adapter proposes a manual action. `Store` keeps `~/.gabitotl/state.json` (injected dir): per file the runtimes that own it, sections, managed keys (with the previous value and a prune flag), hash before/after and version. |
| `internal/runtime` | Core adapter contract (spec 32): `Adapter` (`Detect`, `Inspect`, `Backup`, `Plan`, `Apply`, `Verify`, `RemoveGabitoConfig`, `ListModels`), `Registry`, `Env` (home, project root, state dir, env lookup, `LookPath`, `Exists`, clock: every adapter takes it so tests use temp fixtures), `Detection` (`installed`/`configured`/`detected-history` plus evidence signals) and `Action` (`CREATE`/`MODIFY`/`PRESERVE`/`REMOVE-AFTER-VERIFY`/`NO-ACTION`). `Plan` is pure. `ResolveRuntime(layout)` merges `.gabitotl/runtime.md` with the local override `.gabitotl/local/runtime.md` (local wins); `SetRuntime` writes only those files, so changing the model never touches persona, memory or workflow. A test fails if core code contains runtime names in conditionals. |
| `internal/runtime/engine` | Shared editing machinery used by every adapter: planners (`PlanSection`, `PlanKey`, `PlanServer`, `PlanItem`, `PlanForeignPersona`), `Apply` (CREATE/MODIFY first, verify, then REMOVE-AFTER-VERIFY), `Remove` (driven only by the ownership state, so uninstalling never removes what GabitoTL did not create, and shared files wait for their last owner), global `Backup` under `~/.gabitotl/backups/<ts>/<runtime>/` (touched files only). |
| `internal/runtime/claudecode`, `opencode`, `kiro` | The only packages that know file formats and binary names (formats verified against official docs, assumptions listed in each package doc). Claude Code: persona as a marked section of `~/.claude/CLAUDE.md` or project `CLAUDE.md`, memory MCP in `~/.claude.json` / `.mcp.json`, Gentle-AI persona conflict reported and retired only with `--migrate-persona`. OpenCode: XDG_CONFIG_HOME, OPENCODE_CONFIG, OPENCODE_CONFIG_DIR, JSON and JSONC; persona file plus `instructions` item plus `mcp` entry as managed keys; `model` only when chosen in runtime.md. Kiro: `KiroAdapterCore` shared by `KiroCLI` and `KiroIDE`; one steering file and one MCP entry regardless of how many variants are selected. |
| `internal/runtime/all` | Composition root for adapters: the only importer of the concrete adapter packages. |
| `internal/importer` | Read-only knowledge importers (Kiro `.kiro/*`, Gentle-AI state and managed-block detection, Engram detection and project counts through the T2 adapter, generic CLAUDE.md/AGENTS.md sections outside gabitotl/gentle markers). They return `Knowledge` items (decision, convention, architecture, task, command, constraint, tooling), skip secret-looking files and never modify the source. `imports.md` (`Registry`) records origin, date, content hash, what was imported and last review; an unchanged hash is not imported again. Gentle-AI and Engram are detected independently. |

## Dependency rule

```text
cmd -> cli -> identity -> gabitotl
          cli -> memory/providers -> memory/{engram,mcp} -> memory -> gabitotl
          cli -> scanner -> memory
          cli -> vcs -> gabitotl
          cli -> backup -> gabitotl
          cli -> coordination -> {vcs, memory, gabitotl}
          cli -> budget -> {persona, memory, gabitotl}
          cli -> persona            (leaf: imports nothing from the project)
          cli -> runtime/all -> runtime/{claudecode,opencode,kiro} -> runtime/engine -> {runtime, ownership}
          cli -> importer -> {gabitotl, ownership}
                  runtime/* and importer -> core (never the reverse)
```

- Core packages (`gabitotl`, `identity`, `memory`, `vcs`, `backup`, `coordination`, `persona`, `budget`, `runtime`) never
  branch on runtime names (`if claude`, `switch runtime`). A runtime is an
  opaque string in `runtime.md`; `internal/runtime` has a test that scans core sources for runtime names in conditionals.
- Runtime-specific behavior (file formats, install locations, model names)
  lives only in adapters, behind interfaces owned by the core.
- Core depends on no adapter; adapters may depend on core.
- `persona` is a leaf (it must not import `budget`, which imports it); `vcs` and
  `backup` know nothing about memory; only `coordination` and `budget` touch the
  memory contract, and only through `memory.Provider` / `memory.Recorder`.
- External commands (`git`, `gh`) run only behind `vcs.Runner`; no other package
  shells out to a VCS. Tests use temp repos and fakes, never a real remote.
- User-facing Spanish wording (OJO/TIP, Qué hice/Progreso/Qué sigue, the
  long-chat TIP) lives only in `internal/persona/messages_es-AR.txt`.

## Project layer invariants

- Every write under `.gabitotl/` goes through `gabitotl.WriteFile` /
  `WriteFileIfMissing`, which run the secret guard first and replace files
  atomically. Only `env:NAME`, `keychain:NAME` and `native` references are allowed.
- `Init` is idempotent and never overwrites existing files.
- `local/`, `.bak/` and `tmp/` are listed in `.gabitotl/.gitignore` and never shared.
- IDs used in paths (user, task) must be canonical slugs, so paths cannot escape their directory.

## Memory rule

The workflow depends on `memory.Provider` only. Concrete providers
(`memory/engram`, `memory/mcp`) are registered into a `memory.Factory` at the
composition root and are never imported by workflow code. Team mode requires an
`actor` on every saved entry; historical or unknown authorship is recorded as
`legacy`, never invented.

## Ownership and runtime safety

- GabitoTL never takes over a file it does not own. Markdown assets live in marked sections; JSON/JSONC assets are spliced in as managed keys recorded in `~/.gabitotl/state.json`. Removal restores the previous value of a key we overwrote, drops parents we created, and deletes files we created once they hold nothing else.
- Every `Plan` is pure; `runtime apply` takes a global backup of the touched files first. A Gentle-AI/Gentleman persona block is removed only after verification and only with `--migrate-persona`.
- Equivalent MCP entries (same command and args under any name) are never duplicated, and an entry GabitoTL did not create is never removed.
- Context budget (`context_limit_tokens`, default 200000; `context_threshold`, default 0.75) lives in `workflow.md` front matter; files without the keys keep the defaults (`budget.FromWorkflow`).
