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

## Quick start (host)

```text
gabitotl memory server install --dry-run     # shows exactly what would be created
gabitotl memory server install               # network + volume + container, waits until ready
gabitotl memory serve --http 127.0.0.1:8765  # the MCP server (reads the managed credentials)
gabitotl memory token create --user gabi     # prints the token ONCE
gabitotl memory token create --user gonza
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
  the next request. Tokens are not scoped to a project: anyone with a token can read
  and write every project on that server, so run one server per trust boundary.
- **Limits.** Request body 1 MiB (413 above), title 300 B, content 64 KiB, other
  fields 8 KiB, 200 files, search capped at 20 results (default 8) and always scoped
  to one project, 20 s per tool call, 10 s to read headers.
- **Rate limiting.** 20 requests/s per user (burst 40); ten failed authentications per
  minute per client address, after which that address gets 429 until the window
  refills. Behind a proxy the address is the proxy's: the throttle then applies to
  the proxy as a whole, so keep it in front of the server only if you accept that.
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
- `GABITOTL_TEST_PG_DSN=postgres://...` points the PostgreSQL tests at a throwaway
  database instead; each test creates and drops its own `gtl_test_*` schema.
