# Quick start

From nothing to a verified Run Receipt in about ten minutes. Windows 10 22H2+ or Windows 11, no administrator rights.

## 1. Install

Run `RUNTIDE-<version>-setup.exe` (per-user), or unzip `RUNTIDE-<version>-portable.zip`. Details: [INSTALLATION.md](INSTALLATION.md).
Check it works:

```powershell
runtide version
runtide diagnostics isolation      # which isolation levels can run on this machine
```

## 2. Register a project

A project is a directory you want tasks executed in (usually a git repository). Registering changes nothing in it.

```powershell
runtide project create shop --root C:\work\shop
```

(Desktop: **Projects → New project**.)

## 3. Describe the task

Save as `workspace.yaml`:

```yaml
version: 1
task: Build and test the shop
isolation: managed            # managed | worktree | docker | wsl
start:
  - {name: build, command: npm, args: [run, build]}
verify:
  name: acceptance
  checks:
    - {name: build-output, type: file, path: dist/index.html}
    - {name: unit-tests, command: npm, args: [test], timeout_sec: 300}
```

```powershell
runtide workspace create build --project shop --config workspace.yaml
```

RUNTIDE prints exactly what creating the workspace will do and asks before doing it.

## 4. Approve the commands

Nothing runs until **you** approve the exact commands. Review them, then:

```powershell
runtide workspace inspect shop/build
runtide workspace approve shop/build
```

Editing the configuration later revokes the approval. Agents cannot approve.

## 5. Run, verify, export

```powershell
runtide run create --workspace shop/build "Build and test the shop"
runtide run list                                  # note the execution id
runtide run start <id>                            # runs the start commands
runtide run verify <id>                           # real checks; exit code 3 unless they all pass
runtide run export <id> -o shop-build.runtide.zip
```

Watch it live in the desktop app (**Executions → Live Activity**) or with `runtide run logs <id> -f`.

## 6. Check and share the evidence

```powershell
runtide receipt verify shop-build.runtide.zip     # integrity and signature state
runtide receipt report shop-build.runtide.zip     # self-contained HTML report
runtide run compare <good-id> <bad-id>            # what changed between two runs
```

Optional signing: `runtide receipt keygen`, then `runtide run export <id> --sign`. A signature proves who held the key, not that
you should trust them; trusted signers are an explicit list ([RUN_RECEIPTS.md](RUN_RECEIPTS.md)).

## 7. Reuse verification

Save the checks once and apply them to any execution:

```powershell
runtide verification save acceptance acceptance.yaml
runtide run verify <id> --profile acceptance
runtide project update shop --default-profile acceptance
```

## 8. Let an AI agent use it (optional)

```powershell
runtide mcp config --level controlled     # prints the exact `claude mcp add …` line; changes nothing
runtide mcp check --level read            # starts the MCP server and exercises it
```

See [MCP_INTEGRATION.md](MCP_INTEGRATION.md) and [CLAUDE_CODE_INTEGRATION.md](CLAUDE_CODE_INTEGRATION.md).

## Where things are

Data: `%LOCALAPPDATA%\Runtide` (database, receipts, backups). Everything is local; nothing is uploaded unless you push a receipt
to a server yourself. Troubleshooting: [TROUBLESHOOTING.md](TROUBLESHOOTING.md).
