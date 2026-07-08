# Configuration

Configuration is loaded in this order:

1. Defaults from `internal/config/config.go`.
2. YAML config.
3. Environment variables.
4. CLI flags.

The effective precedence is: flags > environment variables > YAML config > defaults.

## Config File Loading

Use `--config` to point at a YAML file:

```bash
diskmon daemon --config /etc/diskmon/config.yaml
```

When no config path is supplied, diskmon looks in the current directory for a YAML config named `diskmon`.

Packaged `.deb` and `.rpm` installs use this systemd command:

```text
/usr/bin/diskmon daemon --config /etc/diskmon/config.yaml --db /var/lib/diskmon/diskmon.duckdb
```

Because `--db` is a flag, it overrides the `database` value in `/etc/diskmon/config.yaml` unless you change the unit.

## Settings Reference

| Purpose | YAML key | Environment variable | Flag | Default | Notes |
| --- | --- | --- | --- | --- | --- |
| Database path | `database` | `DISKMON_DATABASE` | `--db` | `diskmon.duckdb` | Required after config merge. |
| Collection interval | `collector.interval` | `DISKMON_INTERVAL` | `--interval` | `60s` | Go duration syntax, must be greater than zero. |
| Drive list | `collector.drives` | `DISKMON_DRIVES` | `--drives` | `[]` | Empty list enables auto-discovery. Env/flag values may be comma-separated. |
| Short SMART self-test schedule | `collector.tests.short` | `DISKMON_TEST_SHORT` | none | empty | Optional cron expression. |
| Long SMART self-test schedule | `collector.tests.long` | `DISKMON_TEST_LONG` | none | empty | Optional cron expression. |
| Web listen address | `web.listen` | `DISKMON_WEB_LISTEN` | `--web-listen` | `127.0.0.1:8976` | Keep localhost unless intentionally exposing the UI. |
| Log level | `log.level` | `DISKMON_LOG_LEVEL` | `--log-level` | `INFO` | Accepted values: `DEBUG`, `INFO`, `WARN`, `ERROR`. |
| Notifications | `notifications` | `DISKMON_NOTIFICATIONS` | none | `[]` | Optional array. Env value may be JSON or YAML. |

`--config` is also a persistent flag, but it selects the config file rather than configuring a runtime setting.

## YAML Example

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

The packaged example lives at `packaging/config/config.yaml` and includes commented examples for HTTP, Slack, and Discord notifications.

## Environment Example

```bash
DISKMON_DATABASE=/var/lib/diskmon/diskmon.duckdb \
DISKMON_INTERVAL=5m \
DISKMON_DRIVES=/dev/sda,/dev/nvme0n1 \
DISKMON_TEST_SHORT="0 2 * * *" \
DISKMON_TEST_LONG="0 3 * * 0" \
diskmon daemon
```

## Flag Example

```bash
diskmon daemon \
  --config /etc/diskmon/config.yaml \
  --db /var/lib/diskmon/diskmon.duckdb \
  --interval 5m \
  --drives /dev/sda,/dev/nvme0n1 \
  --log-level INFO
```

## Validation Rules

`diskmon config validate` checks the merged config after defaults, YAML, environment variables, and flags are applied.

```bash
diskmon config validate --config /etc/diskmon/config.yaml
```

Validation fails when:

- `database` is empty.
- `collector.interval` is zero or negative.
- `web.listen` is empty.
- `collector.tests.short` or `collector.tests.long` is not a valid cron expression.
- `log.level` is not one of `DEBUG`, `INFO`, `WARN`, or `ERROR`.
- A notification entry is missing `name`.
- Notification names are not unique.
- A notification entry has no provider or has more than one provider.
- `http.url` is empty for an HTTP notification.
- Slack or Discord config mixes `webhook_url` with SDK mode fields.
- Slack or Discord SDK mode omits either `bot_token` or `channel_id`.

## SMART Test Schedules

`collector.tests.short` and `collector.tests.long` schedule `smartctl -t short` and `smartctl -t long` runs. The scheduler uses cron expressions and records test runs in the database.

The daemon skips a scheduled test when the same device and test type is already in progress. On daemon startup, unfinished test runs are marked incomplete.

## Notifications

Notifications are optional and outbound-only. They do not secure the web UI.

Each notification entry has:

- `name`: required and unique.
- `enabled`: defaults to `true` when omitted.
- `reasons.pass`: defaults to `true` when omitted.
- `reasons.fail`: defaults to `true` when omitted.
- Exactly one provider: `http`, `slack`, or `discord`.

HTTP provider:

```yaml
notifications:
  - name: http-alerts
    reasons:
      pass: false
      fail: true
    http:
      url: https://notify.example.invalid/diskmon-http-webhook
```

Slack webhook provider:

```yaml
notifications:
  - name: slack-webhook
    reasons:
      pass: false
      fail: true
    slack:
      webhook_url: https://hooks.slack.example.invalid/services/T00000000/B00000000/diskmon-example-webhook
```

Discord SDK provider:

```yaml
notifications:
  - name: discord-sdk
    enabled: false
    discord:
      bot_token: discord-diskmon-example-token
      channel_id: "000000000000000000"
```

Webhook URLs, bot tokens, and channel IDs are secrets. Keep them out of shell history, public logs, screenshots, and bug reports.

## Security And Exposure

Default listen address:

```text
127.0.0.1:8976
```

Explicit exposure examples:

```bash
DISKMON_WEB_LISTEN=0.0.0.0:8976 diskmon daemon
```

```bash
diskmon daemon --web-listen 0.0.0.0:8976
```

The web UI has no built-in auth or TLS. If you expose it beyond localhost, use your own trusted network controls, authentication, and TLS.
