# Memory server operator guide

GabitoTL's team memory (`mode: team`, `provider: memory-mcp`) is served by its own
MCP server over PostgreSQL. This guide is for whoever runs that server. Developers
who only use it need a URL and a token (see "Client role").

## Two roles

| Role | Runs | Needs |
|------|------|-------|
| **Host** | `gabitotl memory server install`, `gabitotl memory serve --http ...`, `gabitotl memory token ...` | Docker or Podman, one machine the team can reach |
| **Client** | the agent runtime of each developer | the server URL (`http(s)://host:port/mcp`) and that developer's token |

For a solo user on one machine the host and the client are the same person and
Engram is still the simpler choice. A team host can also be a plain PostgreSQL
you already run: skip `memory server install` and point `--dsn env:NAME` at it.

## Installer (both roles in one step)

`gabitotl install` (TUI or flags) sets either role up through the same pipeline as everything
else: detect, snapshot first, apply, verify, roll back on failure.

```text
# host: this machine runs the server (needs Docker or Podman; never installed for you)
gabitotl install --memory memory-mcp --memory-role host --user gabi [--memory-invite gonza] [--import-engram] --dry-run
gabitotl install --memory memory-mcp --memory-role host --user gabi [--memory-invite gonza] [--import-engram]

# client: connect to an existing server (URL + the NAME of the variable holding your token)
gabitotl install --memory memory-mcp --memory-role client --memory-url https://memoria.example.com/mcp \
    --memory-token-env GABITOTL_MEMORY_TOKEN [--memory-store-token]
```

- **Host.** Installs the managed PostgreSQL (below), creates your token (scoped to this project,
  stored in `~/.gabitotl/secrets/memory-client.env`, mode 0600) and one token per `--memory-invite`
  (printed once at the end, never stored), writes `.gabitotl/memory.md` (`mode: team`,
  `provider: memory-mcp`, `role: host`, `url`, `token: env:NAME`: references only) and registers the
  bridge in the selected runtimes. It does **not** start the server process: the result tells you to
  run `gabitotl memory serve --http 127.0.0.1:8765` (put it behind TLS to reach it over a network).
  Rollback after a failure removes the container and network this run created and **keeps the data
  volume and the credentials**; a server that already existed is never touched by a rollback.
- **Client.** Writes `memory.md` (`role: client`), registers the bridge and validates with
  `memory doctor` (a server that does not answer rolls the install back; a token that is simply not
  exported yet only warns). `--memory-store-token` copies the token from the variable into the 0600
  file (the TUI offers to take it from a password prompt instead); the token is never put in a
  command line, the repo, a log or a report.
- **Knowledge import.** The project knowledge GabitoTL reads from other harnesses (`AGENTS.md`,
  `CLAUDE.md`, ...) is saved through the authenticated client. No actor is sent: the server derives it
  from the token, so the entries belong to whoever runs the install and keep `source: gabitotl-import`.
  If the server is not running yet (you start it by hand), the install warns that it is *unreachable*,
  records nothing as imported and a re-run changes nothing (no new snapshot); once the server is up,
  run `gabitotl import run`.
- **No container engine.** The plan says so and nothing changes: choose the client role or stay
  with Engram. GabitoTL never installs Docker.
- **Engram memory.** With the host role, `--import-engram` runs the tested migrator over the
  snapshot's safety export (see "Migrating from Engram"). A client cannot import: it has no database
  access, so the host does it.

## Client role

A developer needs two things: the server URL (`https://host:port/mcp`; plain `http://` is accepted
only for `127.0.0.1`/`localhost`) and a token. The runtime config gets ONE managed entry that
runs `gabitotl memory mcp`; switching a project between Engram and team memory retires the old
managed entry (only an entry GabitoTL created, recorded in `~/.gabitotl/state.json`) and never
touches entries you wrote yourself.

The token is read **at call time** from the variable named in `memory.md` (`token: env:NAME`), then,
for runtimes launched without your shell environment, from `~/.gabitotl/secrets/memory-client.env`.
Errors are mapped to something you can act on: *authentication failed* (token rejected or not set),
*server unreachable*, *does not match this client's contract* (wrong endpoint or version), *rate
limited*, *not allowed to use that project*. Reads are retried once on a transport failure; writes
never are.

## The stdio bridge (`gabitotl memory mcp`)

A stdio MCP server that exposes exactly the server's tools (same names, descriptions and input
schemas, taken from the same definitions) and forwards each call over HTTP. Flags:
`--url`, `--token-env NAME`, `--project`, `--root`, `--allow-insecure`; all default to the
`.gabitotl/memory.md` of the project it is started in. It fills a missing `project` from
`project_key`. It has no store and no identity of its own: the server authenticates the token, so
scope and actor rules apply unchanged. Logs go to stderr and never contain tokens or entry content.

## Scoped tokens

```text
gabitotl memory token create --user gonza --projects shop,kraken   # explicit allowlist
gabitotl memory token create --user root --all-projects            # explicit opt-in (warned)
gabitotl memory token list                                          # shows the scope of each token
```

A token must say which one it is (neither flag is a usage error: "no scope" is never read as
"everything"). The server enforces the allowlist on every tool: `memory_search`, `memory_save`,
`memory_update` (a scoped token must name a project, so an update cannot reach another one),
`memory_session_summary` and `memory_active_work` answer *not allowed to use project*; `memory_current_project`
hides a default project the token cannot use and `memory_doctor` omits the cross-project counts.

**Migration note.** Schema migration 0002 adds the columns. Every token that existed before it keeps
working on every project (`all projects` in `token list`); there is no in-place scope edit: create a
scoped token and revoke the old one.

## Behind a proxy

The authentication throttle (ten failed attempts per minute) is per client address. Behind a reverse
proxy every request comes from the proxy, so by default it throttles the proxy as a whole. Opt in:

```text
gabitotl memory serve --http 127.0.0.1:8765 --trusted-proxy-header X-Forwarded-For
```

The throttle and the access log then use the **last** entry of that header (the address the nearest
proxy appended; leading entries are client-supplied and ignored); an absent or invalid value falls
back to the TCP peer. Off by default, because anyone who can reach the server directly could forge the
header: only enable it when the server is reachable exclusively through your proxy.

## Migrating from Engram

```text
gabitotl memory migrate engram --project shop --export ~/engram-export.json --dry-run   # counts only
gabitotl memory migrate engram --project shop --export ~/engram-export.json --yes       # import + verify
gabitotl memory migrate engram --project shop --yes                                     # makes the safety export first
```

Run it on the machine that can reach the database (the host). Input is the **official**
`engram export` JSON (without `--export` it runs `engram export` for you, the only command GabitoTL
ever runs against Engram besides `doctor` and `projects list`). Rules:

- **Safety.** It refuses to run unless a backup export exists, is a regular non-empty file and
  looks like an export (`--backup`, default the export itself). Engram's database and files are only
  read; nothing is modified or deleted there.
- **Idempotent and resumable.** Every observation is keyed by a stable source id (`engram:<sync_id>`,
  or a content hash for exports without ids). Re-running never duplicates, never overwrites, never
  resurrects an entry that was deleted afterwards; an interrupted run is simply re-run (the report
  checkpoint says `resumed`).
- **What is kept.** Project, type, content, `topic_key`, original created/updated timestamps; the
  original Engram type, session and relations go to the entry's `meta` (stored, not shown by the tools);
  session summaries become session summaries.
- **Actor.** The original author when the content carries a GabitoTL `**Actor**` trailer (entries
  GabitoTL itself saved); otherwise `legacy` with `source: engram/gentle-ai`. Engram records no author,
  so an actor is never invented. `legacy` cannot authenticate: imports write through the store.
- **What is skipped (and reported).** Soft-deleted observations; **personal-scope** ones (they would
  become visible to the whole team; `--include-personal` opts in); other projects (with `--project`);
  empty ones. Prompts are counted but not imported.
- **Type mapping.** GabitoTL types pass through; `architecture`->`decision`, `pattern`->`convention`,
  `learning`/`manual`->`discovery`, `session_summary`->`progress`, anything else ->`discovery`, always
  with the original kept in `meta` and listed in the report.
- **Topic keys never overwrite team memory.** A `topic_key` already used by a live entry of the project is
  dropped from the imported entry (recorded in `meta`) and counted in the report.
- **Verification.** Counts must add up and a deterministic sample of up to 25 entries is compared
  by content hash, project, type and creation time. The report (`import-report.json`, mode 0600, counts and
  reasons only, no content) is kept under `~/.gabitotl/migrations/<id>_memory-import/`.

The export schema this is built on, with what was verified and what is assumed, is in the package doc of
`internal/memory/migrate/engrammcp`.

## Quick start (host)

```text
gabitotl memory server install --dry-run     # shows exactly what would be created
gabitotl memory server install               # network + volume + container, waits until ready
gabitotl memory serve --http 127.0.0.1:8765  # the MCP server (reads the managed credentials)
gabitotl memory token create --user gabi --projects shop     # prints the token ONCE
gabitotl memory token create --user gonza --projects shop,kraken
```

`serve` resolves its connection through `--dsn env:NAME` (default
`env:GABITOTL_MEMORY_DATABASE_URL`): the process environment first, then, for that
one variable, the managed credentials file. A connection string is never accepted
on the command line.

Other commands: `memory server status|start|stop|logs|backup|remove`,
`memory token list|revoke`. `--instance NAME` gives a trial or parallel instance
its own container, volume, network and credentials file.

## What the managed PostgreSQL is, and what it never touches

| Resource | Name | Notes |
|----------|------|-------|
| Container | `gabitotl-memory-postgres` | `postgres:17.11-alpine`, `--restart unless-stopped`, `no-new-privileges` |
| Volume | `gabitotl-memory-pgdata` | the data; kept by `remove` |
| Network | `gabitotl-memory-net` | dedicated bridge |
| Credentials | `~/.gabitotl/secrets/memory-server.env` | mode 0600, directory 0700 |
| Backups | `~/.gabitotl/backups/memory-postgres/` | mode 0600 dumps |

Everything created carries `io.gabitotl.managed=true` and a role label. The
guarantees (enforced in `internal/pgmanaged`, tested with a fake engine and,
opt-in, with the real one):

- It **never** connects to, stops, restarts, removes, or modifies a container,
  volume or network without that label. If something with our name exists without
  our label, `install` and every other command refuse and tell you so; nothing is
  adopted.
- It **never** uses a PostgreSQL you already run: a dedicated container, a
  dedicated database (`gabitotl_memory`) and role, a dedicated volume. It ignores
  `PGHOST`, `PGPORT`, `PGPASSWORD`, `DATABASE_URL`, `POSTGRES_*` and `~/.pgpass`.
- It publishes on **`127.0.0.1` only**, default port **54329**. If that port is
  busy it picks the next free one (up to 54398); an explicit `--port` that is busy
  fails instead. Port 5432 is rejected outright.
- The password is generated (256 random bits), written to the 0600 file, and handed
  to the container through the environment of the engine command, never as a
  process argument. It is not printed by any command. Note that anyone who can run
  `docker inspect` on this machine can read a container's environment: Docker
  access is root-equivalent, as usual.
- `remove` keeps your data (volume and credentials). `remove --purge-data` deletes
  both and needs you to type the volume name.

Machines with many Compose projects can exhaust Docker's default network address
pools ("all predefined address pools have been fully subnetted"). `install` then
creates its network with a small explicit `10.213.x.0/28` subnet; the engine
rejects any overlap and no other network is inspected.

## Security model of the HTTP endpoint

- **Identity.** Each developer has a token (`gbt_<id>.<secret>`). The actor of every
  write is the owner of the token. A request that names a different `actor` is
  rejected (and logged), never silently rewritten. `legacy` can never be an
  authenticated actor; it exists only for imported history.
- **Tokens.** Created with `memory token create --user ID`, shown once, stored as a
  SHA-256 hash, compared in constant time; a wrong id and a wrong secret are
  indistinguishable. `memory token revoke --user ID [--id TOKEN_ID]` takes effect on
  the next request. Tokens carry a project allowlist (see "Scoped tokens"); an
  `--all-projects` token can use every project on the server.
- **Limits.** Request body 1 MiB (413 above), title 300 B, content 64 KiB, other
  fields 8 KiB, 200 files, search capped at 20 results (default 8) and always scoped
  to one project, 20 s per tool call, 10 s to read headers.
- **Rate limiting.** 20 requests/s per user (burst 40); ten failed authentications per
  minute per client address, after which that address gets 429 until the window
  refills. Behind a proxy see "Behind a proxy" (`--trusted-proxy-header`, off by default).
- **TLS.** Optional: `--tls-cert FILE --tls-key FILE`. Plain HTTP on a non-loopback
  address is refused, because bearer tokens would cross the network unencrypted.
  Put the server behind a TLS-terminating proxy and pass `--allow-insecure-remote`
  to declare that, or bind to `127.0.0.1` and tunnel (for example over SSH).
  Localhost requests with a foreign `Host` header are rejected (DNS-rebinding
  protection of the SDK).
- **Logs.** Structured JSON on stderr: method, path, status, duration, client address,
  user, tool name and outcome. Never entry content, queries, tokens, headers or
  connection strings.
- **Stateless.** Every call is independent (no sessions, no server streams), so there
  is nothing to hijack and the server restarts without losing client state.
- **Stdio.** `memory serve --stdio` serves one trusted local client; the actor is
  `--actor` or the active identity of the project. It writes nothing but the protocol
  to stdout.

## Tools

`memory_search`, `memory_save`, `memory_update`, `memory_session_summary`,
`memory_current_project`, `memory_doctor`, `memory_active_work`. Saving with a
`topic_key` revises the live entry with the same key in that project instead of
duplicating it (concurrent saves are safe). Updates accept `expected_revision` for
an optimistic lock. Deletes are soft; there is no delete tool yet. The client
provider in `internal/memory/mcp` maps to exactly these names.

## Backup and restore

```text
gabitotl memory server backup       # custom-format pg_dump, 0600, under ~/.gabitotl/backups/memory-postgres/
```

Restore into a scratch database first to check a dump (verified procedure):

```text
docker exec gabitotl-memory-postgres createdb -U gabitotl_memory restore_check
docker exec -i gabitotl-memory-postgres pg_restore -U gabitotl_memory -d restore_check --no-owner < DUMP
docker exec gabitotl-memory-postgres psql -U gabitotl_memory -d restore_check -c "select count(*) from observations"
docker exec gabitotl-memory-postgres dropdb -U gabitotl_memory restore_check
```

To restore over the live database use `pg_restore --clean --if-exists -d gabitotl_memory`
the same way. Tokens live in the same database, so a restore brings them back too.
To move to a new machine: install there, restore the dump, copy nothing else: the new
instance has its own credentials.

## Troubleshooting

| Symptom | Meaning |
|---------|---------|
| "a container named ... already exists and was not created by GabitoTL" | The name is taken by something else. Nothing was touched. Use `--instance NAME` or remove the other container yourself if it is yours to remove. |
| "the requested port is already in use" | Pick another with `--port`, or omit it. |
| "credentials file is missing" | The volume has data but its password file is gone. Restore the file, or `remove --purge-data` to start over. |
| `status` shows `ready: no` | `memory server logs`; the container may still be initializing. |
| 429 from the server | Per-user rate limit or too many failed authentications from that address. |

## Tests

Default `go test ./...` never needs Docker. Opt-in suites:

- `GABITOTL_TEST_DOCKER=1` starts one throwaway container named
  `gabitotl-test-<random>-pg` (own volume and network, labeled, removed at the end)
  for the PostgreSQL conformance and engine tests. `GABITOTL_TEST_PG_IMAGE` overrides
  the image.
- `GABITOTL_TEST_DOCKER=1` also runs the client transport and the Engram migrator against the real store.
- `GABITOTL_TEST_PG_DSN=postgres://...` points the PostgreSQL tests at a throwaway
  database instead; each test creates and drops its own `gtl_test_*` schema.
