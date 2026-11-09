# Contributing to RUNTIDE

Thanks for helping. RUNTIDE manages processes on people's machines, so correctness and safety matter more than speed.

## Setup (Windows 10/11)

Requirements: Go (see `go.mod`), Node.js 22+, the WebView2 runtime, and for the desktop build the Wails CLI
(`go install github.com/wailsapp/wails/v2/cmd/wails@latest`). No C compiler is needed.

```powershell
git clone <your fork>; cd RUNTIDE
cd frontend; npm ci; npm run build; cd ..      # go:embed needs frontend/dist
go test ./cmd/... ./internal/... -count=1
go build -o dist\runtide.exe .\cmd\runtide
wails dev                                      # desktop app with hot reload
```

Use explicit package paths (`./cmd/... ./internal/...`), not `./...`: `frontend/node_modules` contains a stray Go package.

## Before opening a PR

```powershell
gofmt -l cmd internal build tests/e2ehost main.go     # must print nothing
go vet ./cmd/... ./internal/... ./tests/e2ehost/... ./build/... .
golangci-lint run ./cmd/... ./internal/... ./tests/e2ehost/... ./build/... .
go test ./cmd/... ./internal/... -count=1
cd frontend; npm run typecheck; npm run lint; npm run format:check; npm test
```

UI changes: also run the Playwright suite (`docs/TESTING.md`).

## Rules

* **No fake functionality.** No hard-coded process lists, random numbers or placeholder buttons. Mocks live in tests only.
* Win32 calls belong in `internal/platform/windows`. Feature packages must not import `app`, `api` or `desktop`.
* Never use `taskkill`, never build shell command strings from input, never elevate.
* Anything destructive needs an explicit confirmation path and tests that prove nothing happens without it.
* Tests must not touch unrelated user processes or require administrator rights.
* Add tests with the change; describe what you ran and the real results in the PR.

## Commits

Small, focused commits with imperative messages. Be kind: see [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
