# Self-hosting the RUNTIDE server

`runtide-server` stores and serves **Run Receipts** and execution history for a team. It never executes commands: there is no
endpoint that runs anything, and receipts it stores are only verified and parsed, never replayed.

## Quick start (local)

```bash
runtide server start          # runs runtide-server.exe from next to runtide.exe, listening on 127.0.0.1:8484
runtide server status
```

On first start the server creates the administrator account and prints the generated password **once** on stderr (set
`RUNTIDE_ADMIN_PASSWORD` to choose it instead). Change it with `POST /api/v1/users/{id}/password`.

```bash
runtide-server.exe --addr 127.0.0.1:8484 --data C:\runtide-server-data
```

| Flag / environment variable | Meaning |
| --- | --- |
| `--addr` / `RUNTIDE_SERVER_ADDR` | listen address, default `127.0.0.1:8484` |
| `--data` / `RUNTIDE_SERVER_DATA` | data directory (SQLite database + receipt files) |
| `--tls-cert`, `--tls-key` / `RUNTIDE_SERVER_TLS_CERT`, `_KEY` | serve HTTPS directly (TLS 1.2+) |
| `--allow-remote` / `RUNTIDE_SERVER_ALLOW_REMOTE=1` | permit a non-loopback address |
| `--behind-tls-proxy` / `RUNTIDE_SERVER_BEHIND_TLS_PROXY=1` | acknowledge that a TLS-terminating proxy protects a plain-HTTP listener |
| `--max-upload-mb` | receipt upload limit (default 256) |
| `RUNTIDE_ADMIN_PASSWORD` | initial administrator password (otherwise generated) |

## Safe defaults

* **Loopback only.** Any other address is refused unless `--allow-remote` is given.
* **No credentials over plain HTTP.** A non-loopback listener additionally requires `--tls-cert/--tls-key`, or
  `--behind-tls-proxy` to state that a proxy terminates TLS. Otherwise the server refuses to start.
* **No unauthenticated API** apart from `/healthz`, `/api/v1/openapi.json` and `/api/v1/auth/login`.
* Passwords use argon2id; tokens are random and stored only as hashes; session tokens expire after 12 hours; API tokens can be
  created with an expiry and revoked.
* Login is rate limited (per IP and per username) and the whole API has a per-client request limit; request bodies other than
  receipts are limited to 1 MiB.
* Every security-relevant action is written to the audit log (`GET /api/v1/audit`, admins).

## Roles

* Global role: `admin` (users, audit), `member`, `viewer`.
* Per project: `owner`, `writer`, `reader`. Uploading requires `writer`; deleting, membership changes require `owner`. The last
  owner of a project cannot be removed.

## Docker

```bash
cp deployments/.env.example deployments/.env     # edit it; never commit it
docker compose -f deployments/docker-compose.yml --env-file deployments/.env up -d --build
docker compose -f deployments/docker-compose.yml logs runtide-server   # first-start admin password, if generated
```

The compose file publishes the port on the host's **loopback only**, runs the container as non-root with a read-only root
filesystem and a named volume for `/data`. Put a TLS-terminating reverse proxy (Caddy, nginx, Traefik) in front of it to expose it.

**Verification status:** the Dockerfile and compose files have **not been built or run** (no Docker engine was running on the
development machine). What *was* verified is everything the image contains: the Linux binary builds with `CGO_ENABLED=0` (an
earlier revision of the code could not, because the server imported Windows-only packages; that was found and fixed) and it runs
correctly in a real Linux environment (WSL2). CI builds the image and probes `/healthz`; check that result before relying on it.

## Uploading

```bash
set RUNTIDE_SERVER_TOKEN=<api token>
runtide receipt push result.runtide.zip --server https://runtide.example.com --project <name-or-id>
```

The server verifies the receipt (integrity, format, path safety) before storing it and rejects anything invalid.

## Logging

One JSON record per request on stderr (`time`, `level`, `msg: "request"`, `id`, `method`, `path`, `status`, `bytes`, `duration`,
`user`, `client`) and an `X-Request-Id` response header with the same id. Authentication failures and rate limits log at `WARN`,
server errors at `ERROR`. Bodies, tokens, passwords and query strings are **never** logged (tested).

## Linux

The server is the only RUNTIDE binary that builds for Linux (the engine, capsules and desktop are Windows only):

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o runtide-server ./cmd/runtide-server
```

`tests/scenario` builds that binary and runs it **inside WSL2 Ubuntu** against a receipt produced on Windows: health check,
authentication required, login, project creation, upload and storage of the Windows receipt, report generation, refusal of a
truncated archive, structured log lines. Receipts therefore verify across operating systems.

## Concurrency

Uploads, reads and project changes from several users at once are covered by `TestConcurrentUsersDoNotCorruptState` (six users,
a shared project, duplicate receipts): no server errors, each distinct receipt stored exactly once per project. The race detector
was run over the server and store packages.

## Database

The server uses **SQLite** (pure Go, WAL mode) in the data directory. **PostgreSQL is not implemented.** Back up by stopping the
server (or using SQLite's online backup) and copying the data directory. There is no built-in web UI: use the API
([API_REFERENCE.md](API_REFERENCE.md)), the CLI, or the desktop app's receipt import.
