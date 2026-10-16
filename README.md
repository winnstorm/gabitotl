# GabitoTL

An **agent harness** for coding agents. GabitoTL is not an LLM and does not call one:
it is a single Go binary that gives the agent you already use (Claude Code, OpenCode,
Kiro) a persona, a project memory, a backup habit, Git discipline and team
coordination, so a project "remembers how to work" across chats, models and people.

- **Why:** chats end, models change, teammates overlap. The knowledge should live in the
  project and in memory, not in a conversation.
- **What you get:** one orchestrator persona (Gabito, a pragmatic Technical Leader),
  invisible-but-always-present memory, a local backup before every modification,
  Conventional Commits without AI attribution, and one worktree per developer.
- **What it never does:** run an agent swarm, touch a PostgreSQL you already run, store
  secrets, or take over config files it did not create. Solo, it adopts your Engram; for a
  team it can run its own Memory MCP server on an isolated, labeled PostgreSQL container.

> Status: V1 core. Licensed under the [MIT License](LICENSE).
> See [Status and limitations](#status-and-limitations) before relying on it.

## 30-second quickstart

```bash
go install github.com/winnstorm/gabitotl/cmd/gabitotl@latest   # or: make build

gabitotl install --dry-run   # see exactly what would change; nothing is written
gabitotl                     # in a project: onboard it (or show its status)
gabitotl doctor              # read-only health check
```

Without a terminal, mutating commands refuse to run unless you pass `--yes`.
`--dry-run` never mutates anything.

## The installer screen (TUI)

On a terminal, `gabitotl` and `gabitotl install` open a full-screen installer
(Bubble Tea, Lip Gloss, Huh) in Rioplatense Spanish. It walks through:

1. what was detected (`✓` ready, `!` needs attention, `→` will be configured, `×` error);
2. what to do (migrate Gentle-AI, install without touching it, review details, quit);
3. "Tu memoria está segura" (reuse Engram, or go to shared team memory);
4. only the questions that cannot be detected (name, solo or team, commits and PRs), one at a time;
   for a team: "Memoria compartida (recomendado para equipos)" with two roles, host and client
   (see [Team memory](#team-memory)), and, if Engram has memory, import it, keep it as history
   or start empty;
5. which coding agents to configure (detected ones preselected; nothing is installed);
6. the plan (CREATE / MODIFY / PRESERVE / IMPORT / REMOVE AFTER VERIFY / NO ACTION), with the
   exact persona block that would be retired, and an `Aplicar` button;
7. live progress, then the result with a TIP (backup kept, `gabitotl cleanup`).

Quitting (`q`, `ctrl+c`) before applying changes nothing. `ctrl+c` while applying asks the
installer to roll back and waits for it, so the machine is never left half-applied; the
retirement of Gentle-AI is the point of no return and always finishes. It works in 80x24,
follows terminal resizes, and honors `NO_COLOR` (symbols, never color alone, carry the state).

The TUI opens only when stdin **and** stdout are terminals. `--no-tui`, `--yes`, `--dry-run`,
pipes and `migrate` keep the plain text flow; `doctor` is always plain text.

## Core concepts

| Concept | Meaning |
|---------|---------|
| **Runtime** | The tool that runs the agent: `claude-code`, `opencode`, `kiro-cli`, `kiro-ide`. Only adapters know its file formats. |
| **Model** | Which LLM the runtime uses. Chosen in `runtime.md`; changing it never touches persona, memory or workflow. |
| **Persona** | Gabito: Argentine Spanish, short answers, "Qué hice / Progreso / Qué sigue", `OJO` and `TIP` only when they apply. |
| **Memory** | Provider-agnostic contract. Engram (CLI) for one person; GabitoTL's own Memory MCP server over a managed PostgreSQL for a team. |
| **Project** | Your repository plus `.gabitotl/`. |

These five are independent on purpose: swap any one without rewriting the others.

**`.gabitotl/` vs memory.** `.gabitotl/` holds *configuration and working state* that
should be reviewable and versioned (`project.md`, `memory.md`, `runtime.md`, `vcs.md`,
`workflow.md`, `imports.md`, `users/`, `work/`). Memory holds *what the project has learned*
(decisions, bugs, progress). Configuration is never hidden only in memory.

**Truth priority:** current code > `.gabitotl/` > Git > memory > imported docs.
If memory contradicts the code, the code wins and the memory gets fixed.

## Install

```bash
# From source
go install github.com/winnstorm/gabitotl/cmd/gabitotl@latest

# From a checkout
make build        # writes bin/gabitotl
```

Requires Go 1.26 (see `go.mod`) and Git. Engram, Claude Code, OpenCode, Kiro and `gh` are
optional: GabitoTL detects what exists and never installs a missing tool behind your back.

## Commands

| Command | What it does |
|---------|--------------|
| `gabitotl` | On a terminal in a project: the installer screen. Piped: onboard a project without `.gabitotl/`, otherwise a short status. Never starts an agent. |
| `install [--dry-run] [--yes] [--no-tui]` | Detect, snapshot, configure runtimes, verify. Idempotent. Flags: `--runtime`, `--user`, `--name`, `--memory`; team memory: `--memory-role host\|client`, `--memory-url`, `--memory-token-env`, `--memory-store-token`, `--memory-invite`, `--import-engram` (see below). |
| `migrate gentle-ai` | Soft landing from Gentle-AI (see below). Adds `--remove-gentle-ai`, `--migrate-persona`. |
| `doctor` | Read-only health check of install and project. Non-zero exit on errors. |
| `restore [id]` | List snapshots, or restore agent configs from one. |
| `uninstall` | Remove only what GabitoTL installed, driven by its ownership state (including its Git guard hooks; a hook of yours that was chained is restored byte for byte). |
| `cleanup` | Offer to delete expired snapshots and legacy copies (30 days). |
| `init` | Create `.gabitotl/`; `--yes`, `--user-name`, `--mode solo\|team`, `--vcs`. |
| `scan [--json]` | Read-only project map (structure, stack, commands, recent history). |
| `memory doctor \| search` | Memory provider health (team mode: server, token, schema, project, managed container) and relevance search. |
| `memory server install \| start \| stop \| status \| logs \| backup \| remove` | The managed, isolated PostgreSQL of team memory. |
| `memory serve --http ADDR \| --stdio` | Run the Memory MCP server. |
| `memory token create \| list \| revoke` | Per-user, project-scoped bearer tokens. |
| `memory mcp` | The stdio bridge runtimes use in team mode (forwards to the server over HTTP). |
| `memory migrate engram` | Import an Engram export into the team memory (idempotent, verified, Engram untouched). |
| `guard scan [--staged \| --diff REF \| --all] [--history] [--json]` | Read-only authorship scan: LLM branding that credits a tool as author. Exit 1 on findings. |
| `guard fix [--dry-run \| --yes]` | Remove only the attribution lines, after a backup in `.gabitotl/.bak/`. Ambiguous cases are listed for manual review; history is never rewritten. |
| `guard install-hooks \| uninstall-hooks \| status` | `commit-msg` and `pre-commit` Git hooks (chained after any hook you already have). Installed by `init`/`install` unless you decline. |
| `vcs status \| branch --task ID` | Git state, provider, permissions; task branch `feature/<id>`. |
| `backup create \| list \| restore \| prune` | Local snapshots under `.gabitotl/.bak/<user>/`. |
| `worktree add \| list \| remove` | One worktree per developer/task, as a sibling directory. |
| `work start \| update \| status \| overlap` | Active-work files and the soft-lock OJO. |
| `who [--json] [--no-memory] [--no-worktrees]` | Who is active here, in other worktrees and in pushed work (active-work files, shared memory). Advisory; never fails because a source is unavailable. |
| `session start \| touch \| end` | Register an agent session's presence for any memory provider (`--task`, `--branch`, `--areas`, `--anchor-pid`). Silent outside a project. |
| `runtime list \| detect \| plan \| apply \| verify \| remove \| set` | Per-runtime configuration and model choice. |
| `import detect \| run` | Read-only knowledge importers (Kiro, Gentle-AI, Engram, instruction files). |
| `persona print [--stats]` | Print the installed persona prompt. |
| `version`, `help` | |

## Authorship: no AI branding

Authorship belongs to the person who asked for the work. GabitoTL detects LLM branding that
credits a tool as author (any model: co-author trailers, "generated with" lines, author headers,
assistant boilerplate, badges and share links) and removes it or refuses to introduce it, in
commits, pull requests, files, `.gabitotl/` and memory. Plain technical mentions (documenting
a runtime, a model id, a config directory) are not flagged.

- `branding_guard: strip | block | warn | off` in `.gabitotl/workflow.md` (default `strip`: messages are
  cleaned and the removal is reported; files are never rewritten silently, they block until
  `guard fix`). Also `branding_extra_terms` and `branding_allow_paths`.
- Git hooks, `guard scan` and `guard fix`, the persona rule, memory stripping and the Claude Code
  `attribution` settings (managed keys, removed on uninstall) all enforce it. `doctor` has an
  "Autoría/branding" section.
- Existing history is reported by `guard scan --history`, never rewritten.

Details, the exact mode table and the false-positive policy: [docs/GUARD.md](docs/GUARD.md).

## Team memory

Engram is the simple choice for one person. For a team, GabitoTL ships its own Memory MCP
server (Go, official MCP SDK) over a PostgreSQL that **it creates and labels itself**: a
dedicated container, volume and network on `127.0.0.1`, never your PostgreSQL, never removed
along with your data. Runtimes only ever need a stdio command: `gabitotl memory mcp`, which
forwards every call to the server with your token.

```text
gabitotl install --memory memory-mcp --memory-role host --user gabi        # montar el servidor
gabitotl memory serve --http 127.0.0.1:8765                                # run it (behind TLS for a network)
gabitotl memory token create --user gonza --projects shop                  # one scoped token per teammate

gabitotl install --memory memory-mcp --memory-role client \
    --memory-url https://memoria.example.com/mcp --memory-token-env GABITOTL_MEMORY_TOKEN   # conectarme
```

- The actor of every write is the owner of the token; a request naming someone else is rejected.
- A token is limited to the projects it was created for (`--projects a,b`); `--all-projects`
  is an explicit, warned opt-in.
- `.gabitotl/memory.md` holds references only (`url`, `token: env:NAME`); the token itself lives in
  your environment or a 0600 file under `~/.gabitotl/secrets/`.
- With no Docker or Podman the installer says so and offers the client role or Engram; it never
  installs Docker.
- An existing Engram memory can be imported (`--import-engram` on the host, or
  `gabitotl memory migrate engram`): a safety export first, no duplicates on re-runs, original
  timestamps kept, unknown authors recorded as `legacy`, Engram never modified.

Operator guide, security model and troubleshooting: [docs/MEMORY_SERVER.md](docs/MEMORY_SERVER.md).

## Who is here

Several people can use one project, even one working directory, and nothing blocks anyone.
`gabitotl who` answers who is active, doing what, and where, merging three scopes and labelling
each row with its source: live sessions of this directory (`directory`), live sessions of the
other worktrees from `git worktree list` (`worktree`), and pushed state (`remote`: active-work
files and, with memory-mcp, the shared memory). The same person and task seen through several
sources is one row. `--json` prints a stable shape (`self`, `people[]`, `warnings[]`).

A session registers itself when it starts work through `work start|update` or the stdio memory
servers. An agent session on an Engram-only project registers through `gabitotl session`;
the commands are silent outside a project and never fail. To do it automatically with Claude Code,
add this to `.claude/settings.json` (or `~/.claude/settings.json`). It is a manual option for now;
the installer does not write hooks. `$PPID` is the Claude Code process that runs the hook, so
start and end name the same session (on Windows leave `--anchor-pid` out and rely on the 12 h expiry).
`session start` prints a short advisory on stdout, which Claude Code adds to the session context.

```json
{
  "hooks": {
    "SessionStart": [{"hooks": [{"type": "command", "command": "gabitotl session start --runtime claude-code --anchor-pid $PPID"}]}],
    "SessionEnd":   [{"hooks": [{"type": "command", "command": "gabitotl session end --anchor-pid $PPID"}]}]
  }
}
```

Optionally refresh `last_seen` on long sessions with `gabitotl session touch --anchor-pid $PPID`
from a `UserPromptSubmit` hook.

## The Gabito persona

Gabito answers in short, natural Argentine Spanish. Long work ends with three blocks;
`OJO` and `TIP` appear only when non-empty:

```text
**Qué hice:** Agregué el login y dejé el cambio guardado.
**Progreso:** 3 de 5 pasos.
**Qué sigue:** Conectar la API.
**Ojo:** Ana está trabajando ahora sobre src/auth. Conviene coordinar.
**TIP:** Este chat ya viene largo. Dejé el estado guardado: podés abrir uno nuevo.
```

`OJO` prevents mistakes; `TIP` helps you think one step ahead. All user-facing wording
lives in one replaceable file (`internal/persona/messages_es-AR.txt`).

## Migrating from Gentle-AI

`gabitotl migrate gentle-ai` is a soft landing, not a replacement-by-force.

Order: `engram doctor` -> snapshot (including `engram export`) -> import knowledge ->
configure runtimes -> verify -> only then retire the old setup -> verify again.

Safety guarantees:

- Snapshot first: configs and the Engram export are saved under `~/.gabitotl/migrations/<id>/`
  before anything changes; `gabitotl restore <id>` brings them back.
- Engram is adopted, never copied or reinstalled; its database and `~/.engram` are never touched.
- Nothing is retired unless every critical verification passed; a failed verification rolls back.
- Gentle-AI is removed with its own `gentle-ai uninstall --all`, never by hand, and only when
  every agent it manages is one GabitoTL replaces. Otherwise GabitoTL does not run it and tells
  you (OJO) to run Gentle-AI's own uninstall manually.
- The Gentleman persona block is replaced only with `--migrate-persona` (or your explicit answer).
- `~/.gentle-ai/backups`, your skills, secrets and repositories are never removed.

## Project layout

```text
.gabitotl/
  project.md memory.md runtime.md vcs.md workflow.md imports.md
  users/<id>.md          shared developer profiles (never emails)
  work/<user>/<task>.md  active work, shared through Git
  local/                 per-person runtime override and session (git-ignored)
  .bak/<user>/<ts>_<task>/  file snapshots with manifest.md (git-ignored)
```

Code layout and the dependency rule: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Security

- Nothing under `.gabitotl/` may hold a secret. Every write runs a secret guard; credentials
  are references only: `env:NAME`, `keychain:NAME` or `native`.
- Reports, logs and inventories list paths, ids and counts, never remotes or environment values.
- Backups skip `.env*` and key files; commands refuse paths outside the project root.
- No LLM branding anywhere: the authorship guard (hooks, scan, fix, persona rule) keeps tool attribution out of commits, PRs, code and memory.
- History is never rewritten: no force-push, `reset --hard`, amend or rebase. Roll back with a revert commit.
- Remote and PR actions follow `vcs.md` permissions (`allow` / `ask` / `deny`).

## Status and limitations

V1 is deliberately small. Known gaps (details in [docs/ROADMAP.md](docs/ROADMAP.md)):

- The TUI covers `gabitotl` and `gabitotl install`; `migrate`, `restore`, `uninstall` and `cleanup` still use plain numbered prompts.
- Team memory runs on Docker (the managed PostgreSQL was tested with Docker; Podman is supported by
  the code but untested). The server process is started by you (`memory serve`); there is no
  service manager integration yet.
- The Engram import is tested against hand-built fixtures that follow the upstream export schema
  (see `internal/memory/migrate/engrammcp`), not against exports of every Engram version.
- Kiro has not been smoke-tested against the real tool; no `gabitotl update`/`upgrade` yet.
- Bitbucket PRs are a documented stub (`ErrNotImplemented`); GitLab/Azure DevOps have no PR support.
- Kiro and Claude Code config formats are partly assumed; each assumption is listed in the
  package docs (`internal/runtime/claudecode`, `internal/runtime/opencode`, `internal/runtime/kiro`).
- `ListModels` is unsupported for every runtime; the model name is whatever you set.
- Commits, reverts, PRs, the context-budget checkpoint and saving the project map are library
  capabilities for the agent runtimes; they have no dedicated CLI command yet.
- Windows is untested; CI covers Linux and macOS.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Short version: `make test`, Conventional Commits, no
AI attribution trailers, no secrets.

## More docs

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) - packages, invariants
- [docs/DECISIONS.md](docs/DECISIONS.md) - why it is built this way
- [docs/GUARD.md](docs/GUARD.md) - the authorship guard: what it detects, modes, hooks, allowlist
- [docs/MEMORY_SERVER.md](docs/MEMORY_SERVER.md) - running the team Memory MCP server and its managed PostgreSQL
- [docs/ROADMAP.md](docs/ROADMAP.md) - what comes after V1
- [docs/GABITOTL_SPEC_V1.md](docs/GABITOTL_SPEC_V1.md), [docs/GABITOTL_INSTALLER_MIGRATION_SPEC.md](docs/GABITOTL_INSTALLER_MIGRATION_SPEC.md), [docs/GABITOTL_BUILD_PROMPT.md](docs/GABITOTL_BUILD_PROMPT.md) - original specs
