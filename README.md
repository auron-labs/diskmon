# diskmon

`diskmon` is a disk health monitoring daemon and CLI for SMART data collection with an embedded web UI.

![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/aaronflorey/diskmon)

## Install

### From release binaries

Download a binary archive from GitHub Releases, extract it, and move `diskmon` into your `PATH`.
For full DuckDB-backed storage support, use Linux release artifacts (CGO-enabled).

Examples:

```bash
# Linux amd64
curl -L -o diskmon.tar.gz \
  https://github.com/aaronflorey/diskmon/releases/download/vX.Y.Z/diskmon_X.Y.Z_linux_amd64.tar.gz

tar -xzf diskmon.tar.gz
sudo install diskmon /usr/local/bin/diskmon
```

```bash
# macOS arm64
curl -L -o diskmon.tar.gz \
  https://github.com/aaronflorey/diskmon/releases/download/vX.Y.Z/diskmon_X.Y.Z_darwin_arm64.tar.gz

tar -xzf diskmon.tar.gz
sudo install diskmon /usr/local/bin/diskmon
```

### From Linux packages (.deb/.rpm/.apk)

Releases also publish native packages.

```bash
# Debian/Ubuntu
sudo dpkg -i diskmon_X.Y.Z_linux_amd64.deb
```

```bash
# RHEL/Fedora
sudo rpm -i diskmon_X.Y.Z_linux_amd64.rpm
```

```bash
# Alpine
sudo apk add --allow-untrusted diskmon_X.Y.Z_linux_amd64.apk
```

NOTE: You can use reprox for automatic updates via APT/RHEL https://reprox.dev/

For `.deb` and `.rpm` packages:

- systemd unit is installed as `diskmon.service`
- config file is installed at `/etc/diskmon/config.yaml`
- database path defaults to `/var/lib/diskmon/diskmon.duckdb`
- package install creates a `diskmon` system user/group (service currently runs as `root` for SMART access compatibility)
- tmpfiles rules are installed at `/usr/lib/tmpfiles.d/diskmon.conf` to create `/var/lib/diskmon` and `/run/diskmon`
- the systemd unit includes hardening directives (`NoNewPrivileges`, `ProtectSystem`, kernel/proc restrictions)

Service management examples:

```bash
sudo systemctl status diskmon
sudo systemctl restart diskmon
journalctl -u diskmon -f
```

Health check endpoints:

- `GET /healthz` -> liveness (`200`)
- `GET /readyz` -> readiness (`200` when storage is reachable, `503` otherwise)

### Docker (GHCR)

```bash
docker run --rm -p 8976:8976 ghcr.io/<owner>/<repo>:latest daemon
```

## Quick start

```bash
diskmon daemon
```

Useful environment variables:

- `DISKMON_DATABASE` (default: `diskmon.duckdb`)
- `DISKMON_WEB_LISTEN` (default: `0.0.0.0:8976`)
- `DISKMON_INTERVAL` (default: `60s`)
- `DISKMON_TEST_SHORT` (optional cron expression, e.g. `0 2 * * *`)
- `DISKMON_TEST_LONG` (optional cron expression, e.g. `0 3 * * 0`)

Optional YAML configuration:

```yaml
collector:
  tests:
    short: "0 2 * * *"
    long: "0 3 * * 0"
```

Cron values use standard 5-field format (`minute hour day-of-month month day-of-week`).

## Local development

### Backend

```bash
task test
task build-mac
```

### Frontend

```bash
cd webui
bun install
bun run dev
```
