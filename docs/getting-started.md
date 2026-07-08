# Getting Started

This guide gets a developer or operator to a first successful `diskmon` run.

## Prerequisites

- A host where `smartctl` from smartmontools can see the target disks.
- Enough privileges for `smartctl` to query each target device.
- A CGO-enabled `diskmon` build for full DuckDB storage support. Linux release artifacts are CGO-enabled.
- Optional for source development: `mise`, Go, Bun, and Task as pinned in `mise.toml`.

Before relying on diskmon, confirm SMART access directly:

```bash
smartctl --scan-open
sudo smartctl -a -j /dev/sdX
```

Replace `/dev/sdX` with a real device from your system.

## Install From Releases

The repository builds release archives, Linux packages, Homebrew casks, and container images with GoReleaser.

Install with Homebrew:

```bash
brew tap auron-labs/tap
brew install --cask diskmon
```

Homebrew installs the cask from the `auron-labs/homebrew-tap` tap. For full DuckDB-backed storage support, prefer Linux CGO release artifacts or native Linux packages.

You can also install a GitHub Release archive directly:

```bash
# Linux amd64
curl -L -o diskmon.tar.gz \
  https://github.com/auron-labs/diskmon/releases/download/vX.Y.Z/diskmon_X.Y.Z_linux_amd64.tar.gz

tar -xzf diskmon.tar.gz
sudo install diskmon /usr/local/bin/diskmon
```

Use Linux release artifacts for full DuckDB-backed storage. Portable no-CGO builds can compile and start, but storage returns an error at runtime.

## Install Linux Packages

Release artifacts include native Linux packages:

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

For `.deb` and `.rpm` packages:

- The systemd unit is installed as `diskmon.service`.
- The packaged config is installed at `/etc/diskmon/config.yaml`.
- The service uses `/var/lib/diskmon/diskmon.duckdb` for the database.
- The unit runs as `root` because SMART access commonly requires elevated privileges across disks, distros, controllers, and HBAs.
- Tmpfiles rules create `/var/lib/diskmon` and `/run/diskmon`.

Service commands:

```bash
sudo systemctl status diskmon
sudo systemctl restart diskmon
journalctl -u diskmon -f
```

## Docker Image

The release workflow publishes GHCR images. The release image is distroless and does not include `smartctl`, so it is not sufficient for host SMART monitoring by itself.

```bash
docker run --rm \
  -e DISKMON_WEB_LISTEN=0.0.0.0:8976 \
  -p 127.0.0.1:8976:8976 \
  ghcr.io/auron-labs/diskmon:latest daemon
```

Keep the host-side bind local with `-p 127.0.0.1:8976:8976` unless you intentionally expose the UI. Host SMART collection from a container also needs explicit device mappings such as `--device=/dev/sda:/dev/sda` or `--device=/dev/nvme0:/dev/nvme0`.

## First Run

Start the daemon with defaults:

```bash
diskmon daemon
```

Defaults:

- Web UI: `http://127.0.0.1:8976`
- Database: `diskmon.duckdb`
- Collection interval: `60s`
- Drive selection: auto-discovered with `smartctl --scan-open` when no drive list is configured

Open the dashboard:

```bash
xdg-open http://127.0.0.1:8976
```

If `xdg-open` is not available, open the URL manually.

## One-Shot Collection

Use `scan` to collect and store one sample without running the daemon loop:

```bash
diskmon scan
```

A successful scan prints the number of stored samples:

```text
stored 1 sample(s)
```

## Verify The Daemon

Check liveness:

```bash
curl http://127.0.0.1:8976/healthz
```

Check storage readiness:

```bash
curl http://127.0.0.1:8976/readyz
```

Expected responses are JSON:

```json
{"status":"ok"}
```

```json
{"status":"ready"}
```

If `/readyz` returns `503`, see [Troubleshooting](troubleshooting.md).

## Source Development Setup

Install pinned local tools and web UI dependencies:

```bash
mise install
bun install --cwd webui --frozen-lockfile
```

Optional hooks:

```bash
hk install --mise
```

Run the local dev processes in separate terminals:

```bash
mise run dev:daemon
```

```bash
mise run dev:webui
```

`mise run dev:daemon` builds the embedded web UI before starting `go run ./cmd/diskmon daemon`. The Vite dev server fetches `/api/v1` paths from its own origin and this repository does not define a Vite proxy, so use the embedded daemon UI for the least surprising end-to-end test.
