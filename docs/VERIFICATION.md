# Verification

A workspace may define a **verification pipeline** (`verify:` in its configuration) describing what "done" means. RUNTIDE runs it
after the task's commands and records every result as evidence. A task is *verified* only when **every required check ran and
passed**.

```yaml
verify:
  name: acceptance
  fail_fast: false
  checks:
    - {name: build-output, type: file, path: dist/app.js, min_size: 1}
    - {name: unit-tests, command: npm, args: [test], timeout_sec: 300}
    - {name: api-health, type: http, url: "http://127.0.0.1:3000/health", expected_status: 200}
    - {name: e2e, command: npm, args: [run, e2e], needs: [api-health]}
    - {name: lint, command: npm, args: [run, lint], required: false}
```

## Check types

`type` is inferred from the fields when omitted (`command:` → command, `url:` → http, `port` → tcp, `path:` → file).

| `type` | Fields | Passes when |
| --- | --- | --- |
| `command` | `command`, `args`, `dir`, `expected_exit` (default 0), `expect_output` (regexp) | exit code matches and, if set, the combined output matches |
| `http` | `url`, `expected_status` (default 200), `expect_body` | the URL answers the status (and body pattern) within the timeout |
| `tcp` | `host`, `port` | a connection succeeds |
| `file` | `path`, `exists` (default true), `contains`, `min_size` | the file state matches |
| `services_healthy` | `services` (default: all) | those services are running and healthy |
| `no_running_services` | `services` (allowed to run) | no other service of the execution is still running |

Common fields: `required` (default `true`), `needs`, `timeout_sec`, `when_file` (the check is *not applicable* unless that
workspace file exists), `description`.

## Statuses

Each check ends in exactly one status: `passed`, `failed`, `skipped`, `timed_out`, `not_applicable`.

* `needs:` lists checks that must pass first; the pipeline is validated before anything runs (unknown names and cycles are
  rejected). A check whose dependency did not pass is `skipped`, with the reason recorded. With `fail_fast`, the remaining checks
  are skipped after the first required failure.
* `not_applicable` is used when `when_file` is not satisfied.

## Overall verdict

* **failed**: any *required* check failed, timed out **or was skipped**. A required check that never ran does not count as a pass.
* **passed**: at least one check ran and no required check failed, timed out or was skipped.
* **none**: no pipeline, or nothing ran.

Optional (`required: false`) and `not_applicable` checks are shown but do not change the verdict.

## Reusable profiles

A **profile** is a named, stored pipeline you can apply to any execution.

```powershell
runtide verification save acceptance acceptance.yaml --description "build output and unit tests"
runtide verification list
runtide run verify <id> --profile acceptance
runtide project update shop --default-profile acceptance      # used when a workspace has no verify section
```

Rules:

* A profile is **validated when saved** (unknown dependencies, cycles, bad types, no checks are rejected), not when it first runs.
* Resolution order for `run verify`: explicit `--profile`, else the workspace's own `verify` section, else the project's default
  profile, else an error that says so.
* Deleting a profile clears it as a project default; recorded verification results keep their check names.
* Profiles contain commands, so **creating them is a human action**: the MCP server can list and use profiles but never write them.
* Desktop: **Verification → Verification profiles**, and the profile picker next to **Verify** on a running execution.

## Running it

```bash
runtide run verify <id>          # exit code 3 unless the verdict is "passed"
```

Desktop: **Verification** summarises
recent executions; **Executions → (execution) → Verification** shows each check with its error and duration. Command checks run
through the same capsule and recorder as the task, so their output is redacted and bounded like any other.
