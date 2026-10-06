# GabitoTL architecture

Go module `github.com/winnstorm/gabitotl`. Stdlib first; the only third-party
dependency is `gopkg.in/yaml.v3` (front matter).

## Package layout

| Path | Responsibility |
|------|----------------|
| `cmd/gabitotl` | Thin `main`: calls `cli.Run`. No logic. |
| `internal/cli` | Subcommand dispatcher and commands (`init`, `memory doctor|search`, `scan [--root] [--json]`, `version`, `help`; stubs for `install`, `doctor`, `migrate`, `restore`, `uninstall`). Takes an `Env` (stdin/stdout/stderr/cwd/TTY) so commands run in-process in tests. |
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
| `internal/adapters/<runtime>` | (Planned, T4.) Per-runtime installers and renderers. |

## Dependency rule

```text
cmd -> cli -> identity -> gabitotl
          cli -> memory/providers -> memory/{engram,mcp} -> memory -> gabitotl
          cli -> scanner -> memory
                  adapters/* -> core (never the reverse)
```

- Core packages (`gabitotl`, `identity`, and later memory/backup/vcs) never
  branch on runtime names (`if claude`, `switch runtime`). A runtime is an
  opaque string in `runtime.md`.
- Runtime-specific behavior (file formats, install locations, model names)
  lives only in adapters, behind interfaces owned by the core.
- Core depends on no adapter; adapters may depend on core.

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
