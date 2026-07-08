# CLI

The binary entry point is `cmd/diskmon/main.go`, which loads configuration, configures logging, and runs the Cobra root command from `internal/cli`.

## Global Flags

These persistent flags apply to every command:

| Flag | Default | Description |
| --- | --- | --- |
| `--config` | empty | Path to config YAML. |
| `--db` | `diskmon.duckdb` | DuckDB database path. |
| `--interval` | `60s` | Collection interval for the daemon. |
| `--web-listen` | `127.0.0.1:8976` | HTTP listen address for API and web UI. |
| `--drives` | empty | Comma-separated device list. |
| `--log-level` | `INFO` | `DEBUG`, `INFO`, `WARN`, or `ERROR`. |

Flags override environment variables, YAML config, and defaults.

## `diskmon daemon`

Run the continuous monitoring daemon:

```bash
diskmon daemon
```

Startup behavior:

1. Validates the merged config.
2. Resolves configured drives or discovers drives with `smartctl --scan-open`.
3. Opens DuckDB storage.
4. Marks stale in-progress SMART test runs as incomplete.
5. Starts the API and embedded web UI server.
6. Runs an immediate collection cycle.
7. Repeats collection on `collector.interval`.
8. Starts scheduled short/long SMART tests when configured.

Common example:

```bash
diskmon daemon \
  --config /etc/diskmon/config.yaml \
  --db /var/lib/diskmon/diskmon.duckdb \
  --interval 5m \
  --drives /dev/sda,/dev/nvme0n1
```

The daemon exits on unrecoverable startup errors, API server errors, or shutdown signals. On interrupt or `SIGTERM`, it attempts a graceful HTTP shutdown with a 10 second timeout.

## `diskmon scan`

Collect one SMART sample for all configured or discovered drives and store it:

```bash
diskmon scan
```

Successful output:

```text
stored 1 sample(s)
```

If collection fails for every drive, the command returns an error from the SMART collector.

## `diskmon config validate`

Validate the merged config:

```bash
diskmon config validate --config /etc/diskmon/config.yaml
```

Successful output:

```text
config is valid
```

Use this before restarting a packaged service after config changes.

## `diskmon version`

Print the version string:

```bash
diskmon version
```

Local builds default to `dev`. Release builds inject the version with GoReleaser using `-X diskmon/internal/cli.version={{.Version}}`.

## Device Selection

Configured devices can be supplied through YAML, `DISKMON_DRIVES`, or `--drives`:

```bash
diskmon scan --drives /dev/sda,/dev/nvme0n1
```

If no drives are configured, diskmon discovers devices by parsing `smartctl --scan-open` output and retaining `/dev/...` paths.

## Scheduled SMART Tests

Scheduled tests are configured only through config/environment values:

```yaml
collector:
  tests:
    short: "0 2 * * *"
    long: "0 3 * * 0"
```

The daemon runs `smartctl -t short <device>` or `smartctl -t long <device>`, waits based on smartctl output when available, polls the self-test log, and records results.
