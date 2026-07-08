# diskmon Documentation

`diskmon` is a disk health monitoring daemon and CLI. It collects SMART data with `smartctl`, stores samples in DuckDB, evaluates drive health, and serves an embedded Vue web UI plus a small JSON/SSE API.

The default runtime posture is local-only: the web UI listens on `127.0.0.1:8976` unless you opt in to another address.

## Recommended Reading Order

1. [Getting Started](getting-started.md) - install options, first run, and verification.
2. [Configuration](configuration.md) - config files, environment variables, flags, defaults, and validation.
3. [CLI](cli.md) - runtime commands and examples.
4. [API](api.md) - health checks, drive endpoints, and server-sent events.
5. [Architecture](architecture.md) - collection flow, storage model, health evaluation, notifications, and web UI embedding.
6. [Development](development.md) - local tools, tests, builds, CI, and release automation.
7. [Troubleshooting](troubleshooting.md) - common failures and diagnostic commands.

## Key Runtime Facts

- Full DuckDB-backed storage requires a CGO-enabled build. The Linux release builds are CGO-enabled; no-CGO builds return a storage error at runtime.
- `diskmon` shells out to `smartctl`, so host permissions and controller/device support determine whether collection succeeds.
- Linux `.deb` and `.rpm` packages install a `diskmon.service`, `/etc/diskmon/config.yaml`, and state paths under `/var/lib/diskmon` and `/run/diskmon`.
- The packaged systemd unit runs as `root` to maximize SMART compatibility, while retaining systemd hardening and write access only for diskmon state/runtime paths.
- The web UI has no built-in auth or TLS. If you expose it beyond localhost, place it behind trusted network controls, authentication, and TLS.

## Source Evidence

These docs are derived from repository files including `README.md`, `Taskfile.yml`, `mise.toml`, `internal/config/config.go`, `internal/cli`, `internal/api`, `internal/storage/schema.sql`, `packaging/`, `.goreleaser.yaml`, and `.github/workflows/`.
