# Release checklist

Nothing below is automated to publish: a maintainer performs each step.

- [ ] Name/trademark checked (USPTO, EUIPO, npm, PyPI, GitHub); repository location decided
- [ ] Version bumped in `wails.json`, `CHANGELOG.md`; `app.Version` is set by `-ldflags` in `build/package.ps1`
- [ ] `gofmt`, `go vet`, `golangci-lint`, `go test`, frontend checks and Playwright all green (see TESTING.md)
- [ ] `build/package.ps1 -Version X.Y.Z` run on a clean checkout
- [ ] Installer tested on a clean Windows 10 and Windows 11 VM: install, launch, upgrade over previous version, uninstall
- [ ] SHA-256 sums verified; binaries signed (when a certificate exists)
- [ ] Screenshots regenerated and reviewed for personal data
- [ ] README and docs match behaviour
- [ ] Tag and GitHub release created **manually**, attaching installer, portable zip, `runtide.exe` and `SHA256SUMS.txt`
- [ ] Announcement posted only after the above (see LAUNCH_ANNOUNCEMENT.md)

Repository metadata recommendation: description "Local-first Windows control center for dev processes, ports, services,
workspaces, logs and diagnostics." Topics: `windows`, `developer-tools`, `process-manager`, `port-manager`, `golang`, `wails`,
`react`, `typescript`, `devops`, `localhost`, `cli`, `diagnostics`.
