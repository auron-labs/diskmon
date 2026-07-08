# diskmon

`diskmon` is a disk health monitoring daemon and CLI for SMART data collection with an embedded web UI.

![GitHub tag (latest by date)](https://img.shields.io/github/v/tag/aaronflorey/diskmon)

## What diskmon shows

The web UI is built for at-a-glance disk monitoring plus drill-down history:

- drive inventory and overall health state
- temperatures and recent changes
- SMART attributes and normalized/raw values
- historical samples and health transitions
- SMART self-test runs, status, and timestamps

Screenshot placeholders until fresh captures are published:

- `[screenshot placeholder: dashboard drive grid]`
- `[screenshot placeholder: drive detail with SMART history]`
- `[screenshot placeholder: SMART self-test run history]`

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
- package install creates a `diskmon` system user/group, but the packaged service currently runs as `root` because `smartctl` access still commonly requires elevated privileges across distros, device nodes, and controller/HBA combinations
- tmpfiles rules are installed at `/usr/lib/tmpfiles.d/diskmon.conf` to create `/var/lib/diskmon` and `/run/diskmon`
- the systemd unit still keeps hardening enabled by default (`NoNewPrivileges`, `ProtectSystem=full`, `ProtectHome=read-only`, `PrivateTmp`, kernel/control-group/module restrictions, `MemoryDenyWriteExecute`, `RestrictSUIDSGID`, `RestrictRealtime`) while allowing writes only to `/var/lib/diskmon` and `/run/diskmon`

Privilege tradeoff for operators:

- root is used for packaged systemd installs to maximize SMART compatibility without requiring distro-specific capability or udev customization
- before changing the unit to a less-privileged user, first confirm `smartctl` can read every target disk on your hardware/distro stack without `sudo`
- only after that validation should you experiment with a non-root override, keeping the existing hardening directives and writable paths for `/var/lib/diskmon` and `/run/diskmon`
- the web UI is intended to stay local by default; if you change the listen address, treat it as a deliberate exposure decision

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
docker run --rm \
  -e DISKMON_WEB_LISTEN=0.0.0.0:8976 \
  -p 127.0.0.1:8976:8976 \
  ghcr.io/<owner>/<repo>:latest daemon
```

Docker caveats:

- `DISKMON_WEB_LISTEN=0.0.0.0:8976` only makes diskmon listen on all interfaces *inside the container*; `-p 127.0.0.1:8976:8976` is the separate host-side control that keeps the published port local to the host.
- Do not use unscoped `-p 8976:8976` unless you intentionally want the UI reachable on all host interfaces.
- The current GHCR release image is distroless and does **not** include `smartctl`, so the default container image is **not sufficient for host SMART monitoring** on its own.
- Host SMART collection from a container usually also needs explicit host device access such as `--device=/dev/sda:/dev/sda` or `--device=/dev/nvme0:/dev/nvme0`, plus any smallest additional capability your storage stack requires.
- Prefer minimal `--device` mappings over broad privileges. Some environments (USB bridges, RAID HBAs, virtualized disks, NAS/container hosts) may still need extra runtime access; treat `--privileged` only as a last-resort troubleshooting step and with an explicit security review.
- Until a purpose-built image with `smartctl` is published, prefer the native Linux package/systemd install for host disk monitoring.
- Non-CGO builds can start, but DuckDB-backed storage is limited/unavailable at runtime.
- USB enclosures, RAID HBAs, NAS appliances, and VM disks may need extra `smartctl` device arguments outside diskmon's current defaults.

## Quick start

Start the daemon:

```bash
diskmon daemon
```

Defaults:

- web UI URL: `http://127.0.0.1:8976`
- database path: `diskmon.duckdb`
- collection interval: `60s`
- drive selection: auto-discovered when possible, or set explicitly

Run a one-shot collection instead of the daemon:

```bash
diskmon scan
```

Validate config before restarting a service:

```bash
diskmon config validate --config /etc/diskmon/config.yaml
```

## Security and exposure

By default, use diskmon as a local-only UI on `http://127.0.0.1:8976`.

If you choose to expose it beyond localhost, do so intentionally and assume anyone who can reach the port can view drive inventory and health data. Put diskmon behind a trusted network and/or a reverse proxy with your own authentication and TLS.

Explicit opt-in exposure examples:

```bash
DISKMON_WEB_LISTEN=0.0.0.0:8976 diskmon daemon
```

```bash
diskmon daemon --web-listen 0.0.0.0:8976
```

For a safer remote-access pattern, keep diskmon on localhost and publish it through a reverse proxy or SSH tunnel instead of binding it directly to the public internet.

## Configuration

Configuration precedence is: flags > environment variables > YAML config > defaults.

### Common environment variables

- `DISKMON_DATABASE` (default: `diskmon.duckdb`)
- `DISKMON_WEB_LISTEN` (default local URL: `http://127.0.0.1:8976`; expose with care)
- `DISKMON_INTERVAL` (default: `60s`)
- `DISKMON_DRIVES` (comma-separated list such as `/dev/sda,/dev/nvme0n1`)
- `DISKMON_TEST_SHORT` (optional cron expression, e.g. `0 2 * * *`)
- `DISKMON_TEST_LONG` (optional cron expression, e.g. `0 3 * * 0`)
- `DISKMON_LOG_LEVEL` (for example `INFO` or `DEBUG`)
- `DISKMON_NOTIFICATIONS` (JSON array; see notification caveats below)

Example:

```bash
DISKMON_DATABASE=/var/lib/diskmon/diskmon.duckdb \
DISKMON_INTERVAL=5m \
DISKMON_DRIVES=/dev/sda,/dev/nvme0n1 \
DISKMON_TEST_SHORT="0 2 * * *" \
DISKMON_TEST_LONG="0 3 * * 0" \
diskmon daemon
```

### YAML config example

```yaml
database: /var/lib/diskmon/diskmon.duckdb

collector:
  interval: 5m
  drives:
    - /dev/sda
    - /dev/nvme0n1
  tests:
    short: "0 2 * * *"
    long: "0 3 * * 0"

web:
  listen: 127.0.0.1:8976

log:
  level: INFO

notifications:
  - name: ops-webhook
    enabled: true
    reasons:
      pass: false
      fail: true
    http:
      url: https://notify.example.invalid/diskmon-webhook
```

### Flag examples

```bash
diskmon daemon \
  --config /etc/diskmon/config.yaml \
  --db /var/lib/diskmon/diskmon.duckdb \
  --interval 5m \
  --drives /dev/sda,/dev/nvme0n1 \
  --log-level INFO
```

Cron values use standard 5-field format (`minute hour day-of-month month day-of-week`).

## SMART / smartctl troubleshooting

diskmon shells out to `smartctl`, so most collection failures come from host access or smartmontools setup.

Checklist:

1. Install `smartctl` / `smartmontools` on the host.
2. Run diskmon with enough privileges to query the target devices.
3. Verify `smartctl -a -j /dev/sdX` works for a sample drive.
4. Check whether the disk is behind USB, RAID, NVMe translation, or virtualization layers that need special `smartctl` options.
5. Review daemon logs with `journalctl -u diskmon -f` or your container logs.

If `smartctl` works manually but diskmon still cannot read a drive, capture the exact `smartctl` output and host/controller details for a bug report.

## systemd notes

- Package installs place the main config at `/etc/diskmon/config.yaml`.
- The service is designed for unattended collection; restart it after config changes.
- Keep the UI on localhost unless you have a specific operational reason to expose it.
- If you must expose it from systemd, prefer a drop-in override that you can audit easily:

```ini
# /etc/systemd/system/diskmon.service.d/override.conf
[Service]
Environment=DISKMON_WEB_LISTEN=0.0.0.0:8976
```

Then run:

```bash
sudo systemctl daemon-reload
sudo systemctl restart diskmon
```

## Notification configuration caveats

- Notifications are optional and outbound-only; they do not secure the web UI.
- Every notification entry defaults to `enabled: true` unless you explicitly set `enabled: false`.
- `reasons.pass` and `reasons.fail` both default to `true`; set them explicitly if a destination should only receive pass or fail events.
- Notification names must be unique so state and delivery history do not collide.
- Each notification entry must use exactly one provider (`http`, `slack`, or `discord`). Do not combine multiple providers in one entry.
- Webhook URLs, bot tokens, and similar notification values are secrets. Do not put them in CLI flags, shell one-liners, pasted terminal transcripts, or screenshots.
- Protect config and env sources that carry notification secrets: prefer YAML with restricted file permissions, a protected env file, systemd `EnvironmentFile=`, or your platform's secret manager.
- `DISKMON_NOTIFICATIONS` accepts JSON or YAML, but multi-entry notification config is usually safer and easier to review in a protected config file than in shell syntax.
- Use clearly fake placeholders in examples and internal docs. For real troubleshooting, redact webhook URLs, bot tokens, channel IDs, and other notification secrets from logs, screenshots, and bug reports before sharing them.
- Test webhook-based notifications with non-production endpoints first.

Safer patterns:

1. Keep notification secrets in `/etc/diskmon/config.yaml` (or equivalent) with restricted permissions.
2. If you must use environment-based config, load it from a protected env file or systemd `EnvironmentFile=` rather than typing secrets directly into commands.
3. When sharing configs for support, replace notification secrets with obvious fakes such as `https://notify.example.invalid/...` or `xoxb-diskmon-example-token`.

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
