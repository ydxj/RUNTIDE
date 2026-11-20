# API reference

There are two HTTP surfaces. Neither can be used without authentication, except where marked *public*.

## 1. Self-hosted server (`runtide-server`)

Base path `/api/v1`. The machine-readable description is served at `GET /api/v1/openapi.json` (always in sync with the code; the
route table is the source of both). Authenticate with `Authorization: Bearer <token>`; get a session token from
`POST /api/v1/auth/login` (`{"username","password"}`), or create a long-lived API token with `POST /api/v1/tokens`.

| Method | Path | Access | Purpose |
| --- | --- | --- | --- |
| GET | `/healthz` | public | liveness/readiness |
| GET | `/api/v1/openapi.json` | public | OpenAPI document |
| POST | `/api/v1/auth/login` | public | exchange credentials for a 12-hour session token |
| POST | `/api/v1/auth/logout` | user | revoke the calling token |
| GET | `/api/v1/me` | user | the authenticated user |
| GET/POST | `/api/v1/users` | admin | list / create users |
| PATCH/DELETE | `/api/v1/users/{id}` | admin | change role or disabled flag / delete |
| POST | `/api/v1/users/{id}/password` | user | admin, or self with the current password |
| GET/POST | `/api/v1/tokens` | user | list / create API tokens (secret shown once) |
| DELETE | `/api/v1/tokens/{id}` | user | revoke |
| GET/POST | `/api/v1/projects` | user | list accessible / create |
| GET/DELETE | `/api/v1/projects/{id}` | user (owner to delete) | get / delete with its receipts |
| GET | `/api/v1/projects/{id}/members` | user | members |
| PUT/DELETE | `/api/v1/projects/{id}/members/{userId}` | owner | add or change / remove |
| GET | `/api/v1/projects/{id}/executions` | reader | stored executions |
| POST | `/api/v1/projects/{id}/receipts` | writer | upload a receipt (`application/zip`); verified before storing |
| GET | `/api/v1/executions/{id}` | reader | inspect (parsed from the verified receipt) |
| GET | `/api/v1/executions/{id}/receipt` | reader | download the `.runtide.zip` |
| GET | `/api/v1/executions/{id}/events` | reader | timeline (`type`, `q` filters) |
| GET | `/api/v1/executions/{id}/logs/{seq}` | reader | recorded output of one command |
| GET | `/api/v1/executions/{id}/report` | reader | self-contained HTML report |
| DELETE | `/api/v1/executions/{id}` | owner | delete |
| GET | `/api/v1/compare?a=&b=` | reader | compare two stored executions |
| GET | `/api/v1/audit` | admin | audit events |

Errors are JSON `{ "error": { "code", "message" } }` with conventional status codes (400 invalid, 401, 403, 404, 413, 429 with
`Retry-After`).

There is **no endpoint that executes a command**; this is asserted by a test over the OpenAPI document.

## 2. Local engine API

The desktop app, CLI and daemon share one engine (`Engine.Dispatch(method, params)`). The CLI and the MCP server reach it over a
loopback-only HTTP API: it binds `127.0.0.1` on a random port, requires a random bearer token stored in `api.json` in your RUNTIDE
data directory (inside your user profile; protected by that folder's Windows permissions), and rejects foreign `Origin` and `Host` headers. It is an internal interface used
by the bundled clients and **is not a stable public API**; use the CLI with `--json` for scripting.

Methods added in 0.2 (all behind that authentication): `projects.*`, `ws.*` (task workspaces), `runs.*` (executions, events,
output, diff, verify, orphans/cleanup), `receipts.*` (export, verify, inspect, import, report, compare, plan, replay, keygen,
keyinfo). Approval of a workspace's commands (`ws.approve`) is a user action and is not exposed by the MCP server.
