# GabitoTL architecture

Go module `github.com/winnstorm/gabitotl`. Stdlib first; the only third-party
dependency is `gopkg.in/yaml.v3` (front matter).

## Package layout

| Path | Responsibility |
|------|----------------|
| `cmd/gabitotl` | Thin `main`: calls `cli.Run`. No logic. |
| `internal/cli` | Subcommand dispatcher and commands (`init`, `version`, `help`; stubs for `install`, `doctor`, `migrate`, `restore`, `uninstall`). Takes an `Env` (stdin/stdout/stderr/cwd/TTY) so commands run in-process in tests. |
| `internal/version` | Build metadata. |
| `internal/gabitotl` | Project layer: `Layout` (all `.gabitotl/` paths), Markdown + YAML front matter codec (`ParseDoc`, `RenderDoc`), typed configs, secret guard, `Init`, `DetectKind`, `FindRoot`. |
| `internal/identity` | Developer identity: slugged IDs, shared profile in `users/`, active identity in `local/user.md`. Never stores emails. |
| `internal/adapters/<runtime>` | (Planned, T4.) Per-runtime installers and renderers. |

## Dependency rule

```text
cmd -> cli -> identity -> gabitotl
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
