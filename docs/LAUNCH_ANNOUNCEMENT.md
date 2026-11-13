# Launch announcement drafts

**Drafts only. Nothing has been posted. Publish only after the release checklist, and only with real numbers and
screenshots from your own release.**

## LinkedIn

I built RUNTIDE: a local-first control center for everything running on a Windows dev machine.

If your day involves a Next.js server, a Laravel API, MySQL, a queue worker, a few containers and a WSL distro, you know
the questions: what is using port 3000? Why did the API die? Which service has to start first?

RUNTIDE answers them in one native app and one CLI (`runtide`):
- real process and port explorer (owner PID, exposure, conflicts)
- supervised services that clean up their whole process tree
- workspaces: dependency-ordered start/stop with health checks, and an explicit approval before anything runs
- live logs with secret redaction
- `runtide doctor` for port conflicts, missing tools, bad paths and missing env variables

No cloud, no account, no telemetry. Go + SQLite + Wails + React. Open source (MIT).

Early software, feedback very welcome: <repository link>

## Hacker News / Reddit (r/windows, r/golang, r/webdev) style

Title: Show HN: RUNTIDE - a local control center for dev processes, ports and workspaces on Windows

Body: Short description, the problem, what is implemented, what is not (network throughput, signed binaries, Linux/macOS), a
link to the architecture and security model docs, and a direct request for bug reports. Follow each community's
self-promotion rules; do not cross-post in bulk.
