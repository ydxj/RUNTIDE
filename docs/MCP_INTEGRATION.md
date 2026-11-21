# MCP integration

`runtide-mcp.exe` is a [Model Context Protocol](https://modelcontextprotocol.io) server (stdio transport, built on the official Go
SDK) that lets an MCP-capable agent use RUNTIDE: create workspaces, run verification, and read real evidence instead of guessing.
It talks to your local engine and starts one if none is running.

```text
runtide-mcp.exe --level read|controlled|sensitive     (default: controlled)
```

## Permission levels

The level is chosen by **you, when you register the server**; an agent cannot raise it. Tools above the level are not
registered at all.

| Level | Tools |
| --- | --- |
| `read` | `list_projects`, `list_verification_profiles`, `list_workspaces`, `list_executions`, `inspect_execution`, `get_execution_status`, `get_execution_logs`, `get_execution_events`, `inspect_receipt` |
| `controlled` | read + `create_workspace`, `create_execution`, `start_workspace`, `run_verification`, `export_receipt` |
| `sensitive` | controlled + `run_command`, `configure_workspace`, `cancel_execution`, `delete_workspace` |

## What agents cannot do

* **Approve a workspace's commands.** Approval is a human action (`runtide workspace approve`, or the desktop app).
  `create_workspace` and `configure_workspace` only *propose*; `start_workspace` fails until you approve the current commands,
  and any later edit revokes the approval.
* Replay receipts, import receipts, or approve anything else.
* Create, edit or delete **verification profiles** (they contain commands). Agents may list profiles and pass a profile name to
  `run_verification`; humans create them.
* Run an ad-hoc command unless the workspace `policy` has `allow_adhoc: true` (and then only within `policy.allow`/`deny`).
* See secrets: output, arguments and events are redacted by the engine before the MCP layer receives them; receipts contain no
  environment values.

Each tool carries MCP annotations (`readOnlyHint`, `destructiveHint`) matching its level, and every action an agent triggers is
recorded in the execution timeline like any other.

## Registering

Claude Code: see [CLAUDE_CODE_INTEGRATION.md](CLAUDE_CODE_INTEGRATION.md). Codex: see [CODEX_INTEGRATION.md](CODEX_INTEGRATION.md).
Any other client that can launch a stdio MCP server uses the same command line. Without MCP, agents can drive the CLI with
`--json` and the documented exit codes (3 = verification failed / receipt invalid).

## Checking it yourself

```powershell
runtide mcp config --level controlled   # prints the registration command; changes nothing
runtide mcp check --level read          # starts runtide-mcp, performs the handshake, lists tools, makes read-only calls
```

`mcp check` reports 9 tools at `read`, 14 at `controlled` and 18 at `sensitive` on this version.

## Test status

The server is covered by automated tests using the SDK's in-memory client transport against a real engine (tool listing per
level, the human-approval requirement with real evidence returned, argument validation, workspace policy for ad-hoc commands). It has **not** been exercised from the Claude Code
or Codex products themselves.
