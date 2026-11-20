# Execution Capsules

An **execution capsule** is the environment one task's commands and services run in. The isolation level is chosen per
workspace (`isolation:` in its configuration) and is stored with every execution and receipt, so a receipt always says how
much separation its evidence came from.

| Level | Name | What it is | What it does **not** protect |
| --- | --- | --- | --- |
| 1 | `managed` | Native Windows processes in a Job Object (kill-on-close, whole-tree termination), clean environment | Files, network, user profile: it does not isolate anything except the process tree and the environment |
| 2 | `worktree` | Level 1, run in a separate directory: a `git worktree` of the project at a detached HEAD (no branch is created), or a filtered copy for non-git projects | Processes can still reach the rest of your user profile; the directory is not a security boundary |
| 3 | `docker` | Each command runs in a container with only the workspace mounted at `/workspace` | Not a guarantee against malicious code; a configured mount can widen access |
| 4 | `wsl` | Commands run inside a WSL2 distribution | Windows drives stay mounted inside WSL: this is **not** a security boundary |

RUNTIDE never describes any level as "secure". Capsules reduce accidents; they do not sandbox hostile code.

## Environment

By default a command receives a **safe allow-list** of inherited variables (`PATH`, `SystemRoot`, `TEMP`, `USERPROFILE`, …)
plus the workspace's `env:` map and per-command `env`. Your other variables, including secrets in your shell, are not passed.
`inherit_env: true` opts in to passing everything; the receipt then records that fact. Values are never written to receipts
(only variable names).

## Git worktrees (level 2)

* A worktree is created at a detached `HEAD`: no branch is created, and **your checked-out branch and your uncommitted changes are never touched**.
* If the source has uncommitted changes, creation is refused unless you pass `--allow-dirty`; the worktree then starts from the
  last commit (`HEAD`) and does **not** contain those changes. The plan shown before creation says so. The source is never modified.
* Non-git projects get a filtered copy (generated directories such as `node_modules` and build output are skipped, as are
  secret-looking files such as `.env`); very large directories are refused, with advice to use a git repository.
* `workspace remove-dir` deletes only the directory the workspace owns, never the project. It refuses when the worktree has
  uncommitted changes, or when the directory is a plain copy (whose changes cannot be checked against a commit), unless `--force`.

## Docker (level 3)

Configured in the workspace (`docker:` section): `image` (required), `memory`, `cpus`, `network` (`""` or `none`), `ports`
(published on `127.0.0.1` only), `mounts` (extra bind mounts, read-only unless `writable`).

Validation refuses: invalid image references, `network` values other than empty or `none`, mounts that are not absolute or that
expose sensitive host locations (user profile secrets, system directories), and malformed ports. The workspace directory is the
only automatic mount.

**Verification status:** the generated `docker run` command lines and the validation are unit-tested. A real container execution
was **not** run end to end on the development machine (Docker Desktop's engine was not running).

## WSL2 (level 4)

Configured in the `wsl:` section (distribution name, optional user). Commands are wrapped with `wsl.exe -d <distro> --cd …`.
Wrapping and validation are unit-tested; an end-to-end run inside WSL was **not** performed.

## Choosing a level

* Quick, trusted tasks on your own repository: `managed`.
* Anything that edits files and that you want to review or discard: `worktree`.
* Untrusted or dependency-heavy builds where a throwaway filesystem helps: `docker` (needs Docker Desktop).
* Linux-only toolchains: `wsl`.
