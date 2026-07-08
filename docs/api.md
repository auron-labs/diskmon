# API

The daemon serves health checks, JSON API endpoints, server-sent events, and the embedded web UI from the configured `web.listen` address.

Default base URL:

```text
http://127.0.0.1:8976
```

The API has no built-in authentication or TLS.

## Health Checks

| Method | Path | Success | Failure |
| --- | --- | --- | --- |
| `GET` | `/healthz` | `200 {"status":"ok"}` | None for normal liveness. |
| `GET` | `/readyz` | `200 {"status":"ready"}` | `503 {"error":"storage unavailable"}` or `503 {"error":"not ready"}`. |

Use `/healthz` for process liveness and `/readyz` when storage must be reachable.

## Drive Endpoints

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/api/v1/drives` | List drive summaries ordered by device. |
| `GET` | `/api/v1/drives/{id}` | Get one drive detail response with health guidance. |
| `GET` | `/api/v1/drives/{id}/history` | Get up to 200 recent history points. |
| `GET` | `/api/v1/drives/{id}/attributes` | Get attributes from the latest sample. |
| `GET` | `/api/v1/drives/{id}/tests?page=&page_size=` | Get paginated SMART test runs. |

Path IDs are signed integers. Invalid IDs return `400 {"error":"invalid id"}`. Missing drives return `404 {"error":"drive not found"}` from the drive detail endpoint.

## Drive Summary Shape

`GET /api/v1/drives` returns an array of objects with fields from `storage.DriveSummary`:

```json
[
  {
    "id": 1,
    "device": "/dev/sda",
    "model": "Example SSD",
    "serial": "EXAMPLE123",
    "health": "GREEN",
    "temperature": 35,
    "power_on_hours": 1234,
    "last_seen": "2026-07-08T12:00:00Z"
  }
]
```

Nullable values may be `null` when SMART data is unavailable.

## Drive Detail Shape

`GET /api/v1/drives/{id}` returns fields from `storage.DriveDetail` plus optional `health_guidance`:

```json
{
  "id": 1,
  "device": "/dev/sda",
  "model": "Example SSD",
  "serial": "EXAMPLE123",
  "wwn": "",
  "health": "YELLOW",
  "health_score": 65,
  "health_reasons": "TEMP_HIGH_WARN",
  "health_guidance": ["Check drive temperature and airflow."],
  "temperature": 50,
  "power_on_hours": 1234,
  "reallocated_sectors": 0,
  "pending_sectors": 0,
  "uncorrectable_sectors": 0,
  "wear_level": 10,
  "collected_at": "2026-07-08T12:00:00Z",
  "first_seen": "2026-07-08T10:00:00Z",
  "last_seen": "2026-07-08T12:00:00Z"
}
```

Exact guidance text comes from `internal/health/guidance.go`.

## History Shape

`GET /api/v1/drives/{id}/history` returns up to 200 recent samples ordered newest first:

```json
[
  {
    "collected_at": "2026-07-08T12:00:00Z",
    "temperature": 35,
    "power_on_hours": 1234,
    "reallocated_sectors": 0,
    "pending_sectors": 0,
    "uncorrectable_sectors": 0,
    "wear_level": 10
  }
]
```

## Attribute Shape

`GET /api/v1/drives/{id}/attributes` returns attributes from the latest sample:

```json
[
  {
    "attribute_id": 5,
    "name": "Reallocated_Sector_Ct",
    "value": 100,
    "worst": 100,
    "threshold": 10,
    "raw": "0",
    "status": "GREEN"
  }
]
```

Attribute status is classified as:

- `GREEN` when threshold is absent or the normalized value is safely above threshold.
- `YELLOW` when the normalized value is within 10 percent of threshold, with a minimum margin of 1 point.
- `RED` when the normalized value is less than or equal to threshold.

## SMART Test Runs

`GET /api/v1/drives/{id}/tests?page=&page_size=` returns:

```json
{
  "items": [
    {
      "id": 1,
      "test_type": "short",
      "scheduled_at": "2026-07-08T02:00:00Z",
      "started_at": "2026-07-08T02:00:01Z",
      "finished_at": "2026-07-08T02:02:01Z",
      "status": "PASSED",
      "message": "Completed without error"
    }
  ],
  "page": 1,
  "page_size": 10,
  "total": 1
}
```

Pagination behavior:

- `page` defaults to `1` when missing, invalid, or less than 1.
- `page_size` defaults to `10` when missing, invalid, or less than 1.
- `page_size` is capped at `100`.

Known SMART test statuses include `STARTED`, `IN_PROGRESS`, `PASSED`, `FAILED`, `SUCCESS`, `COMPLETED`, `UNKNOWN`, and `INCOMPLETE`.

## Server-Sent Events

Endpoint:

```text
GET /api/v1/events
```

Response headers:

```text
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
```

The stream starts with:

```text
retry: 5000
```

Events use this payload shape:

```json
{
  "id": 123,
  "type": "sample.inserted",
  "device": "/dev/sda",
  "timestamp": "2026-07-08T12:00:00Z"
}
```

Event types currently published by the daemon:

- `sample.inserted` after a sample is stored.
- `test.updated` after a SMART test run record is stored.
- `stream.resync` when the client should refetch state instead of relying on replay.

Reconnect behavior:

- Send `Last-Event-ID: <id>` or `?last_event_id=<id>` to request replay.
- The broker keeps a 256 event replay buffer.
- Invalid, too old, too new, or unavailable replay IDs cause a `stream.resync` control event.
- Idle connections receive `: keepalive` comments every 20 seconds.

## Error Shape

Errors use this shape:

```json
{"error":"message"}
```

Internal errors are logged by the daemon and returned to clients as `500 {"error":"internal server error"}`.
