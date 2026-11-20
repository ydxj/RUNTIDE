# Stacks (formerly "workspaces")

> **0.2 naming:** what RUNTIDE 0.1 called a *workspace* is now a **stack**: a named group of long-running services (`runtide stack …`,
> **Stacks** page). The word *workspace* now means an **execution workspace**, the environment of one task: see
> [EXECUTION_CAPSULES.md](EXECUTION_CAPSULES.md), [EXECUTION_RECORDING.md](EXECUTION_RECORDING.md) and
> [VERIFICATION.md](VERIFICATION.md). Stack files, their schema, stored data and approvals are unchanged.

A stack describes a project's whole local environment. It is a YAML file; RUNTIDE validates it before storing
it and before running it.

```yaml
name: my-fullstack-project          # letters, digits . _ -  (max 63)
description: Next.js + Laravel + MySQL
services:
  - name: db
    type: custom
    command: docker
    args: [compose, up, db]
    healthcheck: { type: tcp, port: 3306, interval_sec: 2 }

  - name: backend
    type: laravel
    command: php
    args: [artisan, serve]
    working_directory: backend     # relative to the workspace root, cannot escape it
    env: { APP_ENV: local }
    env_file: backend/.env         # optional, relative to the root
    depends_on: [db]
    healthcheck: { type: http, url: "http://localhost:8000" }
    restart: { policy: on-failure, max_retries: 3, backoff_ms: 1000, max_backoff_ms: 30000 }
    stop_timeout_sec: 10
    start_timeout_sec: 60

  - name: worker
    command: php
    args: [artisan, queue:work]
    working_directory: backend
    depends_on: [db]
```

The project **root** is not part of the portable YAML: it is chosen when the workspace is imported (default: the
folder containing the file) and stored by RUNTIDE.

## Service fields

| Field | Notes |
| --- | --- |
| `name` | Required, unique in the workspace. |
| `type` | `node nextjs php laravel python django flask java worker custom` (default `custom`). Drives Doctor's runtime checks. |
| `command`, `args` | Executed **directly**, never through a shell. `npm` resolves to `npm.cmd` via PATHEXT. Use `cmd.exe /c ...` explicitly if you need shell features. |
| `working_directory` | Relative to the root. Absolute paths, drive letters, UNC paths and `..` escapes are rejected. |
| `env`, `env_file` | Extra variables on top of RUNTIDE's own environment. |
| `depends_on` | Names of services that must be ready first. Unknown names and cycles are rejected (the cycle path is shown). |
| `healthcheck` | `type: http` (`url`, `expect_status`), `tcp` (`port`, `host`), `process`, or `command` (`command`, `args`; exit 0 = healthy). `interval_sec` (10), `timeout_sec` (3), `start_period_sec`, `failure_threshold` (3). HTTP/TCP checks may only target loopback addresses. |
| `restart` | `policy`: `never` (default), `on-failure`, `always`. `max_retries` (3), `backoff_ms` (1000, doubling), `max_backoff_ms` (30000). RUNTIDE never restarts forever. |
| `stop_timeout_sec` | Grace period before the process tree is killed (default 10). |
| `start_timeout_sec` | How long to wait for readiness (default 60). |

Unknown fields are errors, so typos do not silently disable a setting.

## What "ready" means

* With a health check: the check reports **healthy**. A running process alone is never reported healthy.
* Without one: the process must still be alive after 1.5 s. This is a weaker guarantee, so add a health check for
  anything other services depend on.

## Starting and stopping

`up` computes dependency layers. Services in a layer start in parallel; the next layer starts only when every service of
the previous layer is ready. If any service fails, services started by that `up` are stopped again (rolled back) and the
error names the failing service. `down` uses the reverse order. Only one operation per workspace runs at a time.
You can start a subset: `runtide workspace up shop frontend` also starts what `frontend` depends on.

## Authorisation

Running a workspace executes the commands in it. RUNTIDE therefore requires an explicit, informed approval:

* Importing or saving **never runs anything**.
* The first `up` shows every command line and working directory and asks for confirmation (`-y` to pre-approve in
  scripts). In the app a dialog lists the same information.
* Approval is bound to a hash of the exact definition and root. Any edit revokes it.
* Individual workspace services cannot be started separately until the workspace is approved.

## Import/export

`runtide workspace import file.yaml` validates, previews, then registers. `export` prints the stored YAML without the root.
Because imported files are untrusted input, they are parsed strictly, size-checked by the parser and validated for path
traversal before anything is stored.
