# Using RUNTIDE with Claude Code

> **Status:** these steps follow Claude Code's documented MCP registration, and the RUNTIDE MCP server is tested with the Go SDK
> client. A live session with Claude Code was **not** run during development; treat the steps as documentation to verify on your
> machine.

## 1. Register the MCP server

```bash
claude mcp add runtide -- "C:\Program Files\RUNTIDE\runtide-mcp.exe" --level controlled
```

Use `--level read` for inspection only, or `--level sensitive` to also allow ad-hoc commands, reconfiguration, cancel and delete.
The desktop app's **Integrations** page shows these lines with your install path.

## 2. A typical flow

1. In the desktop app or CLI, register the project: `runtide project create shop --root C:\work\shop`.
2. Ask Claude Code to create a workspace for the task (`create_workspace`). It proposes the commands and verification pipeline.
3. **You** review and approve them: `runtide workspace approve shop/auth` (or the Workspaces page). Claude cannot do this.
4. Claude starts the workspace (`start_workspace`), works, runs `run_verification`, and reads `get_execution_status` /
   `get_execution_logs` for real results.
5. Claude exports a receipt (`export_receipt`); you can inspect, compare or share it.

## 3. Instructions you may add to CLAUDE.md

```markdown
When a task needs to be verified, use the runtide MCP tools: create a workspace with a verification pipeline, ask me to approve
it, run it, and report the real per-check results from get_execution_status. Never claim a task is verified without them.
```

## Without MCP

Claude Code can also run the CLI directly (`runtide run create … --json`, `runtide run verify <id>`; exit code 3 means
verification failed). Prefer MCP for approval-aware flows.
