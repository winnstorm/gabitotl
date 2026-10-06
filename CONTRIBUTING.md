# Contributing

Thanks for helping. GabitoTL is small on purpose; please keep changes focused.

## Setup

```bash
git clone <repo> && cd gabitotl
make build test      # needs Go (see go.mod) and Git
```

## Before you open a PR

```bash
make fmt vet test    # format, go vet, go test ./... (includes internal/e2e); `make check` is what CI runs
```

- Tests never touch the real `$HOME`, a real remote or a real agent. Use temp dirs, fakes and
  the fixture machine in `internal/e2e`.
- Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) first. Core packages must not branch on runtime
  names; format-specific code lives in the runtime adapters.
- Code, comments, tests and docs are in English. User-facing Spanish wording lives only in the
  embedded `messages_es-AR.txt` files.
- Never commit secrets, credentials, connection strings or agent-tooling files (`.claude/`, `CLAUDE.md`,
  `.kiro/`, `.engram/`, `odd/`). `.gitignore` already excludes them.

## Commits and PRs

- Conventional Commits (`feat(scope): ...`, `fix: ...`, `docs: ...`, `test: ...`, `ci: ...`, `chore: ...`).
- No `Co-Authored-By` or AI-attribution trailers in commits or PR descriptions.
- One logical change per commit, tests and docs alongside the behavior. Aim for PRs of about 400
  authored changed lines; split larger work into reviewable slices.
- Never rewrite shared history; use a revert commit.

## Reporting problems

Open an issue with the command, the output and `gabitotl version`. Do not paste secrets or
unredacted config files.
