# Troubleshooting

**Start with `runtide doctor`.** It reports the most common problems with evidence and a fix.

| Symptom | Cause / fix |
| --- | --- |
| Desktop window is blank or fails to open | The WebView2 runtime is missing or damaged. Install the Evergreen WebView2 runtime. |
| "Cannot reach the RUNTIDE engine" | The UI could not talk to the engine. Restart the app; if a stale `runtide daemon` is running use `runtide daemon stop`. |
| `workspace up` says "has not been authorised" | By design. Run it interactively and confirm, or pass `-y` after reviewing the commands. Editing the workspace revokes approval. |
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
