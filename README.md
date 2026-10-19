<p align="center">
  <img src="docs/assets/banner.svg" alt="GabitoTL: your coding agent, with memory, manners and teammates" width="100%">
</p>

<p align="center">
  <strong>A single Go binary that gives Claude Code, OpenCode and Kiro a persona, a project memory, Git discipline and team coordination.</strong>
</p>

<p align="center">
  <img alt="Go 1.26" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white">
  <img alt="License: Apache-2.0" src="https://img.shields.io/badge/license-Apache--2.0-blue">
  <img alt="Runtimes" src="https://img.shields.io/badge/runtimes-Claude%20Code%20%C2%B7%20OpenCode%20%C2%B7%20Kiro%20IDE%2FCLI-blueviolet">
  <img alt="Git hosts" src="https://img.shields.io/badge/git-GitHub%20%7C%20GitLab%20%7C%20Bitbucket-orange">
  <img alt="Memory" src="https://img.shields.io/badge/memory-Engram%20%7C%20Team%20server-blue">
  <img alt="Status" src="https://img.shields.io/badge/status-V1%20core-yellow">
</p>

<p align="center">
  <a href="#-features">✨ Features</a> ·
  <a href="#-how-it-works">🔧 How it works</a> ·
  <a href="#-30-second-quickstart">⚡ Quickstart</a> ·
  <a href="#-install">📦 Install</a> ·
  <a href="#-commands-at-a-glance">🧰 Commands</a> ·
  <a href="docs/GUIDE.md">📖 User guide</a> ·
  <a href="#-roadmap-and-status">🧭 Roadmap</a>
</p>

---

## 🤖 What is GabitoTL

Think of your coding agent (Claude Code, OpenCode or Kiro) as a very capable contractor who forgets everything
when the job site closes for the night. GabitoTL is the site notebook, the house rules and the foreman's
whistle: it gives that agent a memory that survives between chats, a consistent way of working, safe Git habits,
and a view of who else is working on the same project.

GabitoTL is **not an LLM and does not call one**. It is the layer around the agent you already use, so a project
"remembers how to work" across chats, models and people. You keep your agent and your model; GabitoTL adds the
parts that chats and models do not keep.

## ✨ Features

<table>
  <tr>
    <td width="33%" valign="top">🎭 <b>Persona and chat modes</b><br>Gabito, a pragmatic Technical Leader. Chats in 🇦🇷 Argentine Spanish or 🇺🇸 English (chosen at install); <code>normal</code>, <code>lite</code> and <code>full</code> chat modes.</td>
    <td width="33%" valign="top">🧠 <b>Project memory</b><br>Engram for one person, or GabitoTL's own Memory MCP server over PostgreSQL for a team. Chosen per project.</td>
    <td width="33%" valign="top">💸 <b>Token savings</b><br>75% context warning (also from Kiro's own usage %), <code>memory context</code>, <code>run</code> digests, scan cache and <code>stats</code>.</td>
  </tr>
  <tr>
    <td valign="top">🔐 <b>Authorship guard</b><br>No AI branding or co-author trailers in commits, PRs, files or memory. Authorship stays with the person.</td>
    <td valign="top">🚢 <b>Git delivery</b><br>Commit, push, PR and merge from settings you answer once. Stacked PRs for big work; push by milestone on GitHub and GitLab to save CI minutes.</td>
    <td valign="top">👥 <b>Team coordination</b><br><code>who</code>, one worktree per task and advisory <code>OJO</code> overlap warnings. Nothing ever blocks anyone.</td>
  </tr>
  <tr>
    <td valign="top">🧩 <b>Eight skills</b><br>Orchestrator, collector, writer, verifier, memory, team, delivery and terminal. Loaded only when needed.</td>
    <td valign="top">🔎 <b>Optional code index</b><br>Detects CodeGraph or code-review-graph and tells the agent to use it first. Never required.</td>
    <td valign="top">🔄 <b>Gentle-AI migration</b><br>Soft landing: snapshot, import, verify, then retire the old setup into a restorable backup.</td>
  </tr>
  <tr>
    <td valign="top">💾 <b>Backups first</b><br>A local snapshot before every modification, restorable with one command.</td>
    <td valign="top">📦 <b>Single Go binary</b><br>No runtime to install. Also a full-screen installer on a terminal.</td>
    <td valign="top">🩺 <b>Doctor</b><br>A read-only health check of the install and the project.</td>
  </tr>
</table>

## 🔧 How it works

One Core, with thin adapters per runtime. Only adapters know a runtime's file formats; the Core never branches on
runtime names, and memory is a contract that Engram or the team server fulfils.

<p align="center">
  <img src="docs/assets/architecture.svg" alt="Architecture: one Core with adapters for Claude Code, OpenCode, Kiro IDE and Kiro CLI, plus memory providers, .gabitotl config and Git delivery" width="860">
</p>

When the agent works, the skills walk it through a short ladder, only as far as the work needs:

<p align="center">
  <img src="docs/assets/workflow.svg" alt="Workflow: understand, remember, investigate, act, verify, deliver" width="860">
</p>

Truth priority: current code > `.gabitotl/` > Git > memory > imported docs. Code layout and invariants:
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). Core concepts: [User guide](docs/GUIDE.md#core-concepts).

## ⚡ 30-second quickstart

```bash
go install github.com/winnstorm/gabitotl/cmd/gabitotl@latest   # or: make build

gabitotl install --dry-run   # see exactly what would change; nothing is written
gabitotl install --persona-lang en   # optional: Gabito chats in English (default: es)
gabitotl                     # in a project: onboard it (or show its status)
gabitotl doctor              # read-only health check
```

Without a terminal, mutating commands refuse to run unless you pass `--yes`.
`--dry-run` never mutates anything. On a terminal, `gabitotl` opens a full-screen installer
([details](docs/GUIDE.md#the-installer-screen-tui)).

## 📦 Install

```bash
# From source
go install github.com/winnstorm/gabitotl/cmd/gabitotl@latest

# From a checkout
make build        # writes bin/gabitotl
```

Prebuilt binaries for Linux, macOS and Windows (amd64 and arm64) are published on the
[releases page](https://github.com/winnstorm/gabitotl/releases).

Requires Go 1.26 (see `go.mod`) to build, and Git. Engram, Claude Code, OpenCode, Kiro and `gh` are optional:
GabitoTL detects what exists and never installs a missing tool behind your back.

## 🧰 Commands at a glance

| Command | What it does |
|---------|--------------|
| `gabitotl` | Installer screen on a terminal; otherwise onboard the project or show a short status. |
| `install`, `uninstall`, `restore`, `cleanup` | Configure runtimes with snapshots; remove only what GabitoTL installed. |
| `init`, `doctor` | Create `.gabitotl/`; read-only health check. |
| `migrate gentle-ai` | Soft landing from Gentle-AI. |
| `memory ...` | Search, context bundles, compaction, provider change, team server and tokens. |
| `scan`, `index`, `run`, `stats` | Project map, optional code index, noisy-command digests, measured savings. |
| `context ...`, `persona ...` | Context budget (75% rule) and the persona and chat modes. |
| `guard ...` | Authorship scan, fix and Git hooks. |
| `vcs ...` | Status, commit, push, PR, merge, ship, setup and login. |
| `who`, `session`, `work`, `worktree` | Team presence, active work and worktrees. |
| `runtime ...`, `import ...`, `backup ...` | Runtime config, knowledge importers, local snapshots. |
| `version`, `help` | |

Every flag and subcommand: [User guide, Commands](docs/GUIDE.md#commands) or `gabitotl help`.

## 🎭 Persona and chat modes

| Language | How to get it | Long-work blocks |
|---|---|---|
| 🇦🇷 Español (Argentina) — default | `gabitotl install` | *Qué hice / Progreso / Qué sigue* · `OJO` · `TIP` |
| 🇺🇸 English | `gabitotl install --persona-lang en`, or pick it when the installer asks | *What I did / Progress / Next* · *Heads-up* · `TIP` |

Your choice is saved in `~/.gabitotl/persona.md` and kept on reinstall; run `install --persona-lang es|en` again to switch.

Both languages follow the same rules; only the chat voice changes, and code, commits and docs stay in English.
`OJO`/*Heads-up* and `TIP` appear only when they apply. Ask for shorter chat replies with
`gabitotl persona mode lite|full` (back with `normal`); code, commits, PRs and memory are never touched.
More: [persona](docs/GUIDE.md#the-gabito-persona), [chat modes](docs/GUIDE.md#chat-modes).

## 🧠 Project memory

```bash
gabitotl memory use memory-mcp --role host --migrate --dry-run   # plan a move to team memory
gabitotl memory token create --user gonza --projects shop        # one scoped token per teammate
```

The provider is chosen per project by one question ("alone or with other people?"). The team server runs on a
PostgreSQL that GabitoTL creates and labels itself; it never touches yours. Tokens are project-scoped. More:
[Team memory](docs/GUIDE.md#team-memory), [Changing the provider](docs/GUIDE.md#changing-the-provider),
[docs/MEMORY_SERVER.md](docs/MEMORY_SERVER.md).

## 💸 Token savings

```bash
gabitotl run -- go test ./...   # the agent sees the exit code, failures and the tail, not thousands of lines
gabitotl stats --since 7d       # measure your own savings; nothing here is a promised percentage
```

Also: the 75% context rule (Claude Code and Kiro hooks), capped `memory context` bundles, a project map cached
while `HEAD` is unchanged. More: [Saving tokens](docs/GUIDE.md#saving-tokens), [Kiro](docs/GUIDE.md#kiro).

## 🔐 Authorship guard

```bash
gabitotl guard scan --staged   # read-only; exit 1 on findings
gabitotl guard fix --dry-run   # remove only attribution lines, after a backup
```

Detects LLM branding that credits a tool as author and removes it or refuses to introduce it. History is never
rewritten. More: [Authorship](docs/GUIDE.md#authorship-no-ai-branding), [docs/GUARD.md](docs/GUARD.md).

## 🚢 Git delivery

```bash
gabitotl vcs setup                                  # answer the delivery questions once
gabitotl vcs ship --title "feat(auth): add login" --dry-run   # only the plan
```

GitHub and GitLab through `gh` and `glab`, Bitbucket Cloud through its REST API. Work over 400 lines is cut into
stacked PRs automatically, and on GitHub and GitLab the agent pushes by milestone to spare CI minutes. More:
[Delivery settings](docs/GUIDE.md#delivery-settings), [Platforms and login](docs/GUIDE.md#platforms-and-login).

## 👥 Team coordination

```bash
gabitotl who                              # who is active here, in other worktrees and in pushed work
gabitotl worktree add --task ID           # one worktree per developer and task
```

Overlaps raise an advisory `OJO`; nothing blocks anyone. More: [Who is here](docs/GUIDE.md#who-is-here).

## 🧩 Skills

Eight skills are installed next to the persona: `gabito-orquestador`, `gabito-recolector`, `gabito-escritor`,
`gabito-verificador`, `gabito-memoria`, `gabito-equipo`, `gabito-entrega` and `gabito-terminal`. The persona has
one rule, "load the matching skill", so the base prompt stays small. More: [Skills](docs/GUIDE.md#skills),
[at a glance](docs/GUIDE.md#the-skills-at-a-glance).

## 🔎 Code index and 🔄 migration

`gabitotl index status` shows an optional code index (CodeGraph first); it never installs one. Coming from
Gentle-AI? `gabitotl migrate gentle-ai` snapshots, imports, verifies and only then retires the old setup. More:
[Code index](docs/GUIDE.md#code-index-optional), [Migrating](docs/GUIDE.md#migrating-from-gentle-ai).

## 🧭 Roadmap and status

V1 is deliberately small: the core is in place, and some paths are tested only against fakes (Kiro, the real Git
hosts), and Windows is untested. See [Status and limitations](docs/GUIDE.md#status-and-limitations) and
[docs/ROADMAP.md](docs/ROADMAP.md). Also: [Security](docs/GUIDE.md#security),
[Project layout](docs/GUIDE.md#project-layout).

## 🤝 Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Short version: `make test`, Conventional Commits, no
AI attribution trailers, no secrets.

## 📄 License

[Apache License 2.0](LICENSE). See also [NOTICE](NOTICE).

## 📚 Docs

- [docs/GUIDE.md](docs/GUIDE.md) - the full user guide
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) - packages, invariants
- [docs/DECISIONS.md](docs/DECISIONS.md) - why it is built this way
- [docs/GUARD.md](docs/GUARD.md) - the authorship guard: what it detects, modes, hooks, allowlist
- [docs/MEMORY_SERVER.md](docs/MEMORY_SERVER.md) - running the team Memory MCP server and its managed PostgreSQL
- [docs/ROADMAP.md](docs/ROADMAP.md) - what comes after V1
- [docs/GABITOTL_SPEC_V1.md](docs/GABITOTL_SPEC_V1.md), [docs/GABITOTL_INSTALLER_MIGRATION_SPEC.md](docs/GABITOTL_INSTALLER_MIGRATION_SPEC.md), [docs/GABITOTL_BUILD_PROMPT.md](docs/GABITOTL_BUILD_PROMPT.md) - original specs
