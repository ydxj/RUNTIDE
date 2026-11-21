# Troubleshooting

**Start with `runtide doctor`.** It reports the most common problems with evidence and a fix.

| Symptom | Cause / fix |
| --- | --- |
| Desktop window is blank or fails to open | The WebView2 runtime is missing or damaged. Install the Evergreen WebView2 runtime. |
| "Cannot reach the RUNTIDE engine" | The UI could not talk to the engine. Restart the app; if a stale `runtide daemon` is running use `runtide daemon stop`. |
| `stack up` says "has not been authorised" | By design. Run it interactively and confirm, or pass `-y` after reviewing the commands. Editing the workspace revokes approval. |
| "executable not found" for `npm`, `php`, ... | Not on PATH of the process that started RUNTIDE. PATH changes need a restart of RUNTIDE (and the daemon). RUNTIDE never edits PATH. |
| A service stays "starting" | It has a health check that never succeeds, or no check and exits within 1.5 s. Read `runtide logs <service>`; check the health URL/port. |
| Port already in use | Doctor/Ports page show the owner. Stop it, or change the port. Binding `0.0.0.0` and `127.0.0.1` in different processes is flagged as a conflict. |
| "Windows denied access" on terminate | The process belongs to another user, is elevated or protected. RUNTIDE never self-elevates; use an elevated tool yourself if you must. |
| Command line / path empty for a process | Windows did not allow reading it (other user/elevated/protected). Normal. |
| Services keep running after the CLI exits | Intended: they live in the background engine. `runtide daemon stop` stops the engine and every service. |
| Services vanished after the app crashed | Job Objects kill the whole tree when RUNTIDE dies. Restart them. |
| Database corrupt message in Settings | RUNTIDE moved the bad file to `*.corrupt-<time>` and created a fresh database. Restore a backup with `runtide config restore`. |
| `runtide config restore` refuses | Stop the engine first (`runtide daemon stop`, quit the desktop app). |
| SmartScreen warning | Binaries are not code-signed yet. Verify the SHA-256 in `SHA256SUMS.txt`. |
| Notifications do not appear | Windows Focus Assist/Do Not Disturb, or "Native notifications" is off in Settings. |
| Tray icon missing | Explorer restarted (RUNTIDE re-adds it) or it is in the hidden-icons overflow. |

Logs of RUNTIDE itself are written to the console of `runtide daemon run` when started manually; managed services' output
is on the Logs page.
| `workspace start` / `run start` refuses: "not approved" | By design. Review with `workspace inspect <ref>`, then `workspace approve <ref>`. Any `configure` revokes approval. |
| Execution is `INTERRUPTED` | RUNTIDE stopped while it ran. Evidence is kept. Check `runtide run orphans`; `runtide run cleanup` terminates only verified leftovers. |
| Verdict is `failed` though every check passed | A *required* check was skipped (its dependency failed) or timed out; see the reasons printed by `run verify`. |
| `receipt verify` exit code 3 | The archive was changed, is incomplete, uses an unsupported format version or has an unsafe entry. The problems are listed. |
| `receipt replay` is blocked | Read the "blocked:" lines: missing commit in `--project-dir`, withheld secrets (`--env`), or no replayable steps. |
| Worktree creation fails | The project must be a git repository with at least one commit; uncommitted changes need `--allow-dirty` (the worktree starts from the last commit). |
| `runtide-server` refuses to start | Non-loopback addresses need `--allow-remote` and TLS (or `--behind-tls-proxy`). |
| `runtide diagnostics isolation` says Docker is unavailable | The engine is not running or not reachable (Docker Desktop stopped). Docker-isolated executions fail with that reason instead of running natively; start Docker Desktop. |
| An execution refuses a path "outside the workspace through a symbolic link or junction" | A link inside the workspace points elsewhere. RUNTIDE will not read or embed files outside the workspace through links; use a path inside it. |
| A project does not appear in the list | It is archived: `runtide project list --all`, then `project unarchive`. |
| Disk use keeps growing | Execution evidence is kept forever by default. Set `execRetentionDays`, or `runtide run prune --older-than 90`. |
| `run verify` says "no verification defined" | The workspace has no `verify` section and its project has no default profile: use `--profile`, add a `verify` section, or set `project update --default-profile`. |
| `runtide mcp check` fails | `runtide-mcp.exe` must be next to `runtide.exe`; the message says what failed (start, handshake, tool list or call). |
