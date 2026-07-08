# Architecture

`diskmon` is a Go daemon/CLI with an embedded Vue web UI.

## Repository Map

| Path | Responsibility |
| --- | --- |
| `cmd/diskmon` | Binary entry point. |
| `internal/cli` | Cobra commands: daemon, scan, config validate, version. |
| `internal/config` | Defaults, YAML/env/flag merging, validation. |
| `internal/smart` | `smartctl` command execution and JSON parsing. |
| `internal/health` | SMART health rules, scores, and guidance. |
| `internal/storage` | DuckDB schema, migrations, ingest, and queries. |
| `internal/api` | HTTP router, handlers, SSE broker, static UI serving. |
| `internal/notification` | Notification provider wiring and transition-aware dispatch. |
| `web` | Embedded built web assets from `web/dist`. |
| `webui` | Vue 3, Vite, Tailwind frontend source. |
| `packaging` | Linux package config, systemd unit, tmpfiles, package scripts. |

## Runtime Flow

Daemon startup:

1. Load and validate config.
2. Resolve drives from config or `smartctl --scan-open`.
3. Open DuckDB and run schema setup/migrations.
4. Mark previous in-progress SMART test runs as incomplete.
5. Build the SMART collector, health evaluator, SSE broker, and notification dispatchers.
6. Start the HTTP server for API and embedded web UI.
7. Configure scheduled SMART self-tests when cron expressions are present.
8. Run an immediate collection cycle, then repeat on the configured interval.

Collection cycle:

1. For each drive, run `smartctl -a -j <device>`.
2. Parse the smartctl JSON into drive identity and sample fields.
3. Evaluate health with `internal/health`.
4. Insert or update drive identity and sample rows in DuckDB.
5. Publish a `sample.inserted` SSE event.
6. Dispatch configured notifications when health transitions meet notification rules.

## SMART Integration

The SMART collector runs these commands:

```bash
smartctl --scan-open
smartctl -a -j /dev/sdX
smartctl -t short /dev/sdX
smartctl -t long /dev/sdX
smartctl -l selftest -j /dev/sdX
```

`diskmon` does not currently expose configuration for extra `smartctl` device arguments. USB enclosures, RAID HBAs, NAS appliances, virtualized disks, and some NVMe translations may require manual validation outside diskmon.

## Health Model

Statuses and scores:

| Status | Score | Meaning |
| --- | ---: | --- |
| `GREEN` | 95 | No triggered signals and sufficient data. |
| `YELLOW` | 65 | Warning signal without a red signal. |
| `RED` | 20 | Critical signal present. |
| `UNKNOWN` | 0 | Insufficient parsed data. |

Default thresholds:

| Signal | Threshold |
| --- | --- |
| Temperature warning | `>= 50C` |
| Temperature critical | `>= 55C` |
| Wear level degraded | `>= 80%` |

Red signals include failing SMART status, failed attributes, nonzero reallocated sectors, nonzero pending sectors, nonzero uncorrectable sectors, critical temperature, and NVMe critical warnings.

Yellow signals include nonzero UDMA CRC errors, warning temperature, and degraded wear level.

## Storage Model

DuckDB tables are defined in `internal/storage/schema.sql`:

| Table | Purpose |
| --- | --- |
| `drives` | Stable drive identity, device path, model, serial, WWN, first/last seen timestamps. |
| `smart_samples` | Time-series SMART sample summary fields and raw JSON. |
| `smart_attributes` | Per-sample SMART attributes. |
| `drive_health` | Health status, score, and reason list for each sample. |
| `smart_test_runs` | Scheduled SMART self-test lifecycle and results. |
| `notification_state` | Per-drive, per-notification dedupe state. |

Storage uses `github.com/marcboeker/go-duckdb` and requires CGO. When built without CGO, `storage.OpenDuckDB` returns `duckdb storage requires cgo; rebuild with CGO_ENABLED=1 and a linux cross-compiler`.

## API And Web UI

The API uses `go-chi/chi` routes under `/api/v1` and serves SPA assets from the embedded `web/dist` filesystem.

Static serving behavior:

- `/assets/*` serves embedded built assets.
- Non-API paths fall back to `index.html` for the Vue router.
- `/api/*` paths that are not registered return `404`.

Frontend routes:

- `/` renders the dashboard.
- `/drives/:id` renders drive details.

The frontend API client fetches relative `/api/v1` paths.

## Notifications

Notifications are optional. Configured providers are converted into `internal/notification.Entry` values and dispatched through `github.com/nikoksr/notify`.

Supported providers:

- HTTP webhook.
- Slack webhook or Slack SDK mode.
- Discord webhook or Discord SDK mode.

The daemon stores notification state by drive and notification name to avoid repeated sends when a drive remains in the same health state.

## Build And Release Shape

The release build runs web UI install/build hooks before GoReleaser packaging. GoReleaser produces:

- CGO-enabled Linux amd64 and arm64 binaries.
- Portable no-CGO Darwin and Windows archives.
- Linux `.deb`, `.rpm`, and `.apk` packages for Linux builds.
- GHCR Docker images for linux amd64 and arm64.

For runtime monitoring with DuckDB storage, prefer Linux CGO artifacts or packages.
