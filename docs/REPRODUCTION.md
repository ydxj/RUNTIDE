# Reproduction

RUNTIDE classifies every receipt by how faithfully its execution could be reproduced, and says why. The classification is
computed when the receipt is built (`internal/receipts/build.go`) and re-assessed on your machine at replay time.

| Class | When |
| --- | --- |
| `fully_reconstructable` | A replayable workspace configuration was recorded, the source is a **clean** git commit, no ad-hoc commands ran, no command arguments were masked, the parent environment was not inherited, no secret environment values were needed, and the isolation level is not docker/wsl |
| `partially_reconstructable` | Replayable, but at least one of the above does not hold. Each reason is listed: not a git repository, uncommitted changes at start, ad-hoc commands, masked arguments, inherited parent environment, secret environment values withheld, container/distribution contents not captured |
| `metadata_only` | No replayable workspace configuration was recorded (no setup/start/service/verify steps): the commands and results are evidence only |
| `not_reproducible` | The execution ended before any command ran, so there is nothing to reproduce |

Only `Missing` items that RUNTIDE actually knows are missing are reported; the class never claims more than the recording supports.

## Replay

```powershell
runtide receipt replay result.runtide.zip --project-dir C:\work\shop --plan   # pre-flight only
runtide receipt replay result.runtide.zip --project-dir C:\work\shop          # pre-flight, then ask before executing
runtide receipt replay result.runtide.zip --project-dir C:\work\shop --yes    # execute without asking
```

The desktop app does the same under **Receipts → Inspect → Replay pre-flight…**.

### 1. Pre-flight (always first; changes nothing)

* **Dependencies**, each `ok`, `different` or `missing`: tools and versions the original used, secret variables that were withheld
  (supply them with `--env NAME=value`, or the password fields in the desktop app), the source checkout and commit, and the
  runtime for Docker/WSL receipts.
* **Steps** that would run, exactly as configured.
* **Potentially destructive operations**: a *heuristic* scan of the steps for deletion, forced or bulk operations, process
  killing, registry/service/network changes, publishing, `git clean`/`reset --hard`/`push`, `docker rm/prune`, and shell scripts
  whose effect RUNTIDE cannot inspect. It cannot prove a command safe; no warning does not mean safe.
* **Permissions** the replay needs: a new git worktree, running N steps as your Windows user without elevation, long-running
  services that may listen on local ports, and the fact that managed/worktree isolation does not restrict files or network.
* **Blockers**, if any.

### 2. Approval
Replay executes commands that came from a file, so it needs explicit approval of what you were shown (interactive confirmation,
`--yes`, or the approval dialog in the desktop app, which lists the destructive operations again). It cannot be triggered by an
import, and the MCP server does not expose it.

### 3. Run
The steps run in a new `worktree` capsule checked out at the recorded commit **from your local checkout** (`--project-dir`),
never in your working tree, and are recorded as a new execution with its own receipt.

### 4. Outcome
The replay is compared with the original receipt and classified:

| Outcome | When |
| --- | --- |
| **Reproduced successfully** | same final state and verification verdict, matching commands and file changes, and the receipt was fully reconstructable |
| **Partially reproduced** | same verdict, but inputs were known to be missing (class `partially_reconstructable`, for example a different tool version), or commands or file changes differ |
| **Could not reproduce** | the replay did not run or finish, or the final state, the verdict or individual checks differ from the receipt |
| **Metadata only** | the receipt holds evidence but no replayable steps, so nothing was run |

Reproducing a *failure* counts as reproduced. Equal results never prove two runs were identical: the reasons are listed.

### Blockers (replay refuses)

* the receipt is `not_reproducible`/`metadata_only` (no replayable steps) or contains no workspace configuration;
* the embedded configuration no longer validates on this RUNTIDE;
* secret environment variables were withheld: supply them with `--env NAME=value`;
* no `--project-dir`, it is not a git repository, or it does not contain the recorded commit;
* the receipt was recorded on a non-Windows system (this RUNTIDE replays Windows executions only).

## Verification status

Class assessment, planning and blockers are unit-tested, and a real replay in an isolated worktree that produces a comparable
receipt is tested on the same machine that recorded it. A replay of a receipt recorded on a **different** machine has not been exercised.
