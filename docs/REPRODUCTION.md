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

```bash
runtide receipt replay result.runtide.zip --project-dir C:\work\shop --plan   # show the plan only
runtide receipt replay result.runtide.zip --project-dir C:\work\shop          # plan, then ask before executing
runtide receipt replay result.runtide.zip --project-dir C:\work\shop --yes    # execute without asking
```

1. **Plan** (always first; it changes nothing): the locally assessed class and reasons, the exact steps that would run, tool
   version differences (`node: recorded 20.11, installed 22.1`), items not captured, and **blockers**.
2. **Approval.** Replay executes commands that came from a file, so it needs explicit approval of the displayed steps (interactive
   confirmation, or `--yes`). It cannot be triggered by an import, and the MCP server does not expose it.
3. **Run.** The steps run in a new `worktree` capsule checked out at the recorded commit **from your local checkout** (`--project-dir`),
   never in your working tree, and are recorded as a new execution with its own receipt.
4. **Compare.** The replay is compared with the original receipt (verdict transition, new failures, resolved failures). Matching
   evidence does not prove the two runs were equivalent.

### Blockers (replay refuses)

* the receipt is `not_reproducible`/`metadata_only` (no replayable steps) or contains no workspace configuration;
* the embedded configuration no longer validates on this RUNTIDE;
* secret environment variables were withheld: supply them with `--env NAME=value`;
* no `--project-dir`, it is not a git repository, or it does not contain the recorded commit;
* the receipt was recorded on a non-Windows system (this RUNTIDE replays Windows executions only).

## Verification status

Class assessment, planning and blockers are unit-tested, and a real replay in an isolated worktree that produces a comparable
receipt is tested on the same machine that recorded it. A replay of a receipt recorded on a **different** machine has not been exercised.
