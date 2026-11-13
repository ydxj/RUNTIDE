# First contribution ideas

Each is self-contained and testable without administrator rights.

1. **Network throughput** (`internal/platform/windows`, `internal/resources`): add 64-bit per-interface byte counters via `GetIfTable2` and show rates on the Resources page.
2. **More tool detectors** (`internal/tools`): Ruby, .NET SDK, Rust/cargo, Deno/Bun, Redis CLI; add a test using a temp PATH.
3. **Doctor rule: `.env` DATABASE_URL scheme check** (`internal/diagnostics`): flag URLs pointing at a different local port than a running database container.
4. **Doctor rule: duplicate ports across workspaces**.
5. **CLI**: `runtide ports --watch` and `runtide services --watch` using the event stream.
6. **Logs page**: regex search toggle and a "jump to next error" shortcut.
7. **Accessibility**: keyboard resizing for detail panes; verify with Narrator and write findings.
8. **Docs**: Add a "Migrating from docker-compose + npm scripts" guide with a real example workspace.
9. **winget / Scoop manifests** for the portable zip.
10. **Frontend tests** for Workspaces, Settings and Environment pages (fake engine pattern in `frontend/src/test`).
