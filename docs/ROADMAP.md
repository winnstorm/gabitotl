# GabitoTL roadmap (after V1)

Items the specs place after V1, plus the V1 shortcuts that were taken on purpose.
Sources: `GABITOTL_SPEC_V1.md` section 31 ("Después") and
`GABITOTL_INSTALLER_MIGRATION_SPEC.md` sections 44, 45 and 52.

## Deferred on purpose in V1

- **Interactive TUI (Bubble Tea, Lip Gloss, Huh).** The installer spec recommends
  it (section 3), but V1 ships a plain-stdlib presenter: numbered choices, one
  question at a time, state symbols (`✓ ! → ×`) and the Gabito voice. All logic
  lives in `internal/installer`, so a TUI only replaces `internal/installer/ui`
  and the prompts; no behavior moves.
- **Engram to Memory MCP/PostgreSQL migrator.** `internal/memory/migrate` has the
  `MemoryMigrator` interface and an empty registry. A migrator ships only when the
  Memory MCP server is chosen and the adapter is tested against it. Until then the
  installer keeps the Engram export and says so.
- **Concrete Memory MCP server.** The abstract provider exists; no server or
  PostgreSQL access is wired.

## Installer and distribution

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
