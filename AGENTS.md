# Repository Guidelines

## Project Structure & Module Organization
`diskmon` is a Go daemon/CLI with an embedded web UI.
- `cmd/diskmon`: entrypoint binary (`main.go`).
- `internal/cli`: Cobra commands (`daemon`, `scan`, `version`, `config validate`).
- `internal/config`, `internal/smart`, `internal/storage`, `internal/health`, `internal/api`, `internal/util`: core backend modules.
- `web`: embedded static assets served by Go (`go:embed`, `web/dist`).
- `webui`: Vue 3 + Vite + Tailwind source (`src/components`, `src/views`, `src/api`, `src/stores`).
- `build`: generated binaries.

Keep module boundaries clear: collection logic in `smart`, persistence in `storage`, HTTP handling in `api`.

## Build, Test, and Development Commands
Primary commands are in `Taskfile.yml`:
- `task test`: run `go test ./...`.
- `task fmt`: format Go files with `gofmt`.
- `task build-mac`: build native macOS binary.
- `task build-linux-amd64` / `task build-linux-arm64`: CGO Linux cross-builds (GNU/glibc toolchains required).
- `task build-linux-amd64-nocgo`: Linux build without CGO (DuckDB disabled at runtime).
- `task build-all`: build macOS + Linux artifacts.

Frontend (from `webui/`):
- `bun install`
- `bun run dev`
- `bun run build`

## Coding Style & Naming Conventions
- Go style: `gofmt` output is required; keep files focused and readable.
- Package names are short, lowercase nouns (`smart`, `health`, `storage`).
- Exported identifiers use `CamelCase`; unexported helpers use `camelCase`.
- Prefer explicit dependency wiring over global state.
- Vue: keep components in `PascalCase.vue`; colocate API helpers in `webui/src/api`.

## Testing Guidelines
- Use Go’s standard `testing` package.
- Place tests next to code as `*_test.go`.
- Name tests `Test<Behavior>` (example: `TestEvaluateReturnsRedOnCriticalWarning`).
- Run `task test` before opening a PR.

## Commit & Pull Request Guidelines
Git history is not available in this workspace snapshot, so use Conventional Commits:
- `feat: add SMART history endpoint`
- `fix: stop SPA redirect loop`

PRs should include:
- concise summary of behavior changes,
- affected modules/paths,
- test/build commands run,
- screenshots for UI changes (`Dashboard`, `DriveDetail`).

## Configuration & Build Notes
- Config precedence: flags > env > YAML > defaults.
- Key env vars: `DISKMON_DATABASE`, `DISKMON_WEB_LISTEN`, `DISKMON_INTERVAL`.
- DuckDB requires CGO for full functionality; non-CGO builds compile but return a runtime storage error.
