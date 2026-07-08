# Development

This project uses Go for the daemon/CLI and Vue 3 with Vite for the web UI.

## Tooling

Pinned tools in `mise.toml`:

| Tool | Version |
| --- | --- |
| Go | `1.25.0` |
| Bun | `1.3.14` |
| Task | `3.52.0` |
| GoReleaser | `2.17.0` |
| hk | `1.50.0` |
| pkl | `0.31.1` |

Install tools and web UI dependencies:

```bash
mise install
bun install --cwd webui --frozen-lockfile
```

Equivalent mise task for web UI dependency installation:

```bash
mise run install
```

Optional hooks:

```bash
hk install --mise
```

## Local Development

Start the daemon locally:

```bash
mise run dev:daemon
```

This task depends on `mise run build:webui`, which runs `task build-webui`, then starts:

```bash
go run ./cmd/diskmon daemon
```

Start the Vite dev server:

```bash
mise run dev:webui
```

The Vite dev server runs `bun run --cwd webui dev`. The frontend fetches relative `/api/v1` paths and this repository does not define a Vite proxy, so use the embedded daemon UI for an end-to-end browser check unless you add a local proxy outside the repository.

## Checks

Run the aggregate check:

```bash
mise run check
```

Focused checks from `Taskfile.yml`:

```bash
task fmt-check
task vet
task lint-webui
task test-webui
task test
```

What they run:

| Command | Behavior |
| --- | --- |
| `task fmt-check` | Checks `gofmt` output for tracked Go files. |
| `task vet` | Runs `go vet ./...`. |
| `task lint-webui` | Runs `bun run --cwd webui lint`. |
| `task test-webui` | Runs `bun run --cwd webui test:run`. |
| `task test` | Builds the web UI, then runs `go test ./...`. |

Web UI scripts in `webui/package.json`:

```bash
bun run --cwd webui dev
bun run --cwd webui build
bun run --cwd webui preview
bun run --cwd webui lint
bun run --cwd webui test
bun run --cwd webui test:run
```

## Formatting And Fixes

Format Go files:

```bash
task fmt
```

Run repository hook fixers:

```bash
mise run fix
```

## Builds

Build the embedded web UI:

```bash
task build-webui
```

Build binaries:

```bash
task build-mac
task build-linux-amd64
task build-linux-arm64
task build-linux-amd64-nocgo
task build-linux-arm64-nocgo
task build-all
```

Linux CGO cross-build tasks check for these compiler commands unless overridden by environment variables:

| Variable | Default command |
| --- | --- |
| `LINUX_AMD64_CC` | `x86_64-unknown-linux-gnu-gcc` |
| `LINUX_AMD64_CXX` | `x86_64-unknown-linux-gnu-g++` |
| `LINUX_ARM64_CC` | `aarch64-unknown-linux-gnu-gcc` |
| `LINUX_ARM64_CXX` | `aarch64-unknown-linux-gnu-g++` |

The no-CGO Linux tasks are useful compile checks, but DuckDB storage is disabled at runtime in those binaries.

## CI

`.github/workflows/test.yml` runs on pull requests and branch pushes. It performs:

1. Go setup from `go.mod`.
2. Bun setup at `1.3.14`.
3. `bun install --cwd webui --frozen-lockfile`.
4. `bun audit --cwd webui`.
5. `bun run --cwd webui lint`.
6. `bun run --cwd webui test:run`.
7. `bun run --cwd webui build`.
8. `test -z "$(gofmt -l $(git ls-files '*.go'))"`.
9. `go vet ./...`.
10. `go test ./...`.

## Release Automation

`.github/workflows/release.yaml` runs on pushes to `main` and `master`.

Release flow:

1. `googleapis/release-please-action@v4` uses `release-please-config.json` and `.release-please-manifest.json`.
2. If release-please creates a release, the workflow checks out the release tag.
3. The workflow installs Go, Bun, web UI dependencies, and the Linux arm64 cross compiler.
4. It logs in to GHCR.
5. `goreleaser/goreleaser-action@v6` runs `goreleaser release --clean`.

Local snapshot release build:

```bash
mise run release:snapshot
```

That task runs:

```bash
goreleaser release --snapshot --clean
```

## Commit And PR Conventions

Use conventional commits so release-please can create changelog entries and version bumps:

```text
feat: add SMART history endpoint
fix: stop SPA redirect loop
```

Use `feat!:` or a `BREAKING CHANGE:` footer for breaking changes.
