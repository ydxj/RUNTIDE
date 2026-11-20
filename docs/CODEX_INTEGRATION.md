# Using RUNTIDE with Codex

> **Status: documented only, not tested.** No Codex client was available during development. RUNTIDE's MCP server uses the
> standard stdio transport, so any MCP-capable client should be able to launch it, but **compatibility with Codex has not been
> verified** and its configuration format may differ from what is shown. Check Codex's own MCP documentation.

## MCP (stdio)

Register a server that runs:

```text
C:\Program Files\RUNTIDE\runtide-mcp.exe --level controlled
```

with no extra arguments or environment. Codex's configuration file and keys are defined by Codex; consult its documentation for
the exact syntax. Tools, permission levels and the approval rule (agents cannot approve a workspace's commands) are described in
[MCP_INTEGRATION.md](MCP_INTEGRATION.md).

## Without MCP

Any agent that can run shell commands can use the CLI. Use `--json` for machine-readable output and rely on exit codes:

| Command | Exit code 3 means |
| --- | --- |
| `runtide run verify <id>` | the verification verdict is not `passed` |
| `runtide receipt verify <file>` | the receipt is not valid |

Workspace approval still needs you: `runtide workspace approve <ref>`. An agent that runs `runtide workspace start … --yes` is
approving on your behalf; do not allow that in untrusted contexts.
