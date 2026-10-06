# GabitoTL roadmap (after V1)

Items the specs place after V1, plus the V1 shortcuts that were taken on purpose.
Sources: `GABITOTL_SPEC_V1.md` section 31 ("Después") and
`GABITOTL_INSTALLER_MIGRATION_SPEC.md` sections 44, 45 and 52.

## Deferred on purpose in V1

- Done since V1: the Memory MCP server, the managed PostgreSQL, the client transport and stdio bridge,
  project-scoped tokens, the tested Engram migrator and the team memory installer (TUI and flags).

## Still open in team memory

- A service-manager integration for `memory serve` (today you start the process; docs show how to run it
  behind TLS) and a `memory server upgrade` for new PostgreSQL images.
- Podman is supported by the engine code but untested; the managed PostgreSQL was exercised with Docker only.
- The Engram migrator is tested against hand-built fixtures that follow the upstream schema; run it with
  `--dry-run` against your own export first. Relations and the original Engram type are stored in `meta`
  but not yet exposed by the tools.
- Token scope cannot be edited in place (create a scoped token, revoke the old one).

## Authorship guard

- Verify on a real Claude Code that an empty `attribution.commit` / `attribution.pr` hides the trailer (documented
  value is partly assumed, see docs/GUARD.md), and look for attribution options in OpenCode and Kiro as their docs grow.
- A TUI question for the hooks (today: installed by default, `--no-hooks` / `--no-guard-hooks` to decline).
- `guard fix` for files only; an offer to propose reverting commits that carry attribution is out of scope on purpose
  (history is never rewritten).

## Installer and distribution

- Smoke test of the Kiro adapter against the real tool (its formats are verified against documentation only).

- TUI for `migrate`, `restore`, `uninstall` and `cleanup` (V1 ships the TUI for `gabitotl` and
  `gabitotl install`; those commands keep the plain prompts), and a dry-run entry inside the TUI.

- `gabitotl update` / `gabitotl upgrade`: backup, update the binary, migration
  schema, dry reconciliation, apply and verify, touching only the runtimes
  registered in `~/.gabitotl/state.json` (installer spec section 45).
- Package-manager installers: Homebrew tap, winget, scoop/choco, apt/dnf/pacman
  packages (section 44), and bootstrap scripts (`curl | sh`, `irm | iex`) that
  detect the platform, download a release, verify its checksum, install only the
  binary and run `gabitotl install`.
- Installing missing runtimes or Engram on request (today a missing tool is
  reported, never installed behind the user's back).
- GitHub/Bitbucket authentication helpers.
- Per-agent Gentle-AI uninstall: no per-agent form is verified (`gentle-ai
  uninstall -h` documents only `--all`), so GabitoTL never runs one. When `--all`
  would remove an agent GabitoTL does not replace, the user is told to uninstall
  manually. Revisit once a released Gentle-AI CLI documents a narrow form.

## Product

- GitLab and Azure DevOps complete (the PR provider interface exists; only GitHub
  is implemented, Bitbucket is a documented stub).
- Dashboard for memory and progress.
- Additional importers (other harnesses and tools beyond Kiro, Gentle-AI, Engram
  and instruction files).
- More sophisticated soft locks.
- Optional auxiliary agents.
- Cost and token metrics.
- Advanced team policies.
