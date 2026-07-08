# Troubleshooting

Most operational issues come from SMART access, storage build mode, config validation, or intentionally exposing the local-only web UI.

## Quick Diagnostics

Check daemon health:

```bash
curl http://127.0.0.1:8976/healthz
curl http://127.0.0.1:8976/readyz
```

Check logs for packaged installs:

```bash
journalctl -u diskmon -f
```

Validate the config before restarting:

```bash
diskmon config validate --config /etc/diskmon/config.yaml
```

Verify smartctl directly:

```bash
smartctl --scan-open
sudo smartctl -a -j /dev/sdX
```

## `smartctl` Is Missing Or Fails

Symptoms:

- Logs contain `smartctl failed for ...`.
- `diskmon scan` exits with a SMART collection error.
- The daemon logs `collection failed`.

Causes:

- smartmontools is not installed.
- The diskmon process lacks permission to read target devices.
- The disk sits behind USB, RAID, HBA, NVMe translation, NAS, container, or VM layers that need special `smartctl` handling.

Fixes:

1. Install smartmontools for your OS.
2. Confirm `smartctl -a -j <device>` works manually.
3. Run diskmon with sufficient privileges for the same device access.
4. For packaged services, inspect `journalctl -u diskmon -f`.
5. If manual smartctl needs extra device options, note that diskmon does not currently expose config for those extra arguments.

## No Devices Are Discovered

Symptom:

```text
smartctl scan returned no devices
```

Cause:

- No `collector.drives`, `DISKMON_DRIVES`, or `--drives` value is configured, and `smartctl --scan-open` returns no usable `/dev/...` paths.

Fixes:

1. Run `smartctl --scan-open` manually.
2. Configure explicit drives:

```yaml
collector:
  drives:
    - /dev/sda
    - /dev/nvme0n1
```

Or use a flag:

```bash
diskmon daemon --drives /dev/sda,/dev/nvme0n1
```

## No Successful Collection Results

Symptom:

```text
no successful collection results
```

Cause:

- Diskmon attempted all configured/discovered drives and every `smartctl -a -j <device>` collection failed or could not be parsed.

Fixes:

1. Check logs for per-device `smart collection failed` warnings.
2. Validate one device manually with `sudo smartctl -a -j /dev/sdX`.
3. Remove stale or unsupported devices from `collector.drives`.
4. Confirm the daemon has the same permissions as the successful manual command.

## `/readyz` Returns 503

Symptoms:

```json
{"error":"storage unavailable"}
```

```json
{"error":"not ready"}
```

Causes:

- Storage was not wired into the handler.
- DuckDB cannot open or query the configured database path.
- The binary was built without CGO.

Fixes:

1. Check the database path in the effective config.
2. Confirm the process can write to the database directory.
3. Use a Linux CGO-enabled release artifact or package for full storage support.
4. Avoid no-CGO builds for runtime monitoring with DuckDB.

The no-CGO storage error is:

```text
duckdb storage requires cgo; rebuild with CGO_ENABLED=1 and a linux cross-compiler
```

## Config Validation Fails

Run:

```bash
diskmon config validate --config /etc/diskmon/config.yaml
```

Common errors:

| Error | Fix |
| --- | --- |
| `database path is required` | Set `database`, `DISKMON_DATABASE`, or `--db`. |
| `interval must be greater than zero` | Set `collector.interval` to a positive duration such as `60s` or `5m`. |
| `web listen address is required` | Set `web.listen` or `DISKMON_WEB_LISTEN`. |
| `collector.tests.short must be a valid cron expression` | Use a valid cron expression such as `0 2 * * *`. |
| `unsupported log level` | Use `DEBUG`, `INFO`, `WARN`, or `ERROR`. |
| `notifications[n].name is required` | Add a unique `name` to every notification. |
| `notifications[n] must configure exactly one provider` | Configure only one of `http`, `slack`, or `discord`. |

## Web UI Is Not Reachable Remotely

Default behavior:

```text
127.0.0.1:8976
```

This binds only to localhost. To expose intentionally:

```bash
DISKMON_WEB_LISTEN=0.0.0.0:8976 diskmon daemon
```

Or:

```bash
diskmon daemon --web-listen 0.0.0.0:8976
```

The web UI has no built-in auth or TLS. Prefer a trusted network, reverse proxy, authentication, TLS, or SSH tunnel instead of binding directly to a public interface.

For systemd, use a drop-in override and restart:

```ini
# /etc/systemd/system/diskmon.service.d/override.conf
[Service]
Environment=DISKMON_WEB_LISTEN=0.0.0.0:8976
```

```bash
sudo systemctl daemon-reload
sudo systemctl restart diskmon
```

## Docker Container Cannot Monitor Host Disks

Symptoms:

- `smartctl` is not found inside the container.
- No host disks are visible.
- Collection fails even though the HTTP server starts.

Causes:

- The release image is distroless and does not include `smartctl`.
- Host devices are not mapped into the container.
- Additional device permissions or capabilities may be required for the host storage stack.

Fixes:

1. Prefer native Linux package/systemd installs for host disk monitoring.
2. If using containers, build or use an image that includes smartmontools.
3. Map only required devices, such as `--device=/dev/sda:/dev/sda`.
4. Keep published ports local unless intentionally exposing the UI.

## Scheduled SMART Tests Stay Unknown Or Incomplete

Symptoms:

- SMART test run status is `UNKNOWN`.
- SMART test run status is `INCOMPLETE` after daemon restart.
- Logs mention a previous test is still in progress.

Causes:

- `smartctl -l selftest -j` did not return a matching result.
- The test result did not change from the baseline before polling ended.
- The daemon restarted while a test run was in progress.
- The device reports another test already in progress.

Fixes:

1. Check `journalctl -u diskmon -f` around the scheduled time.
2. Run `smartctl -l selftest -j <device>` manually.
3. Increase spacing between short and long cron schedules.
4. Avoid scheduling overlapping tests on the same device.

## API Errors

Common API error responses:

| Status | Response | Meaning |
| ---: | --- | --- |
| `400` | `{"error":"invalid id"}` | The `{id}` path parameter is not an integer. |
| `404` | `{"error":"drive not found"}` | The drive detail endpoint has no row for that ID. |
| `503` | `{"error":"event stream unavailable"}` | SSE broker is unavailable. |
| `500` | `{"error":"internal server error"}` | Internal details were logged server-side and sanitized for the client. |

For SSE clients, a `stream.resync` event means the client should refetch state through the JSON endpoints before reconnecting.
