# quickget-agent API

## Starting the agent

```bash
quickget-agent serve
```

By default, the agent listens on:

- `127.0.0.1:19329`

Base URL:

- `http://127.0.0.1:19329`

## Authentication

All endpoints except `GET /health` require a bearer token:

- Header: `Authorization: Bearer <local-token>`

Token storage:

- Default token file: `<user-config-dir>/QuickGet/agent-token`
- On Windows, this is typically: `%AppData%/QuickGet/agent-token`

You can print or create the token with:

```bash
quickget-agent token
```

Security note:

- Treat this token like a local password.
- Do not share it in screenshots, logs, bug reports, or chat.

## Endpoints

### `GET /health`

Health check (no auth required).

Example:

```bash
curl http://127.0.0.1:19329/health
```

Response:

```json
{
  "ok": true,
  "name": "quickget-agent",
  "version": "dev"
}
```

### `GET /downloads`

List all download jobs.

Example:

```bash
curl -H "Authorization: Bearer $QG_TOKEN" \
  http://127.0.0.1:19329/downloads
```

Response:

```json
[
  {
    "id": "d_abc123",
    "url": "https://example.com/file.iso",
    "outputPath": "C:/Downloads/file.iso",
    "status": "running",
    "downloaded": 1048576,
    "total": 734003200,
    "percent": 0.14,
    "speedMBps": 12.5,
    "avgMBps": 10.8,
    "error": "",
    "message": "downloading",
    "createdAt": "2026-05-16T10:00:00Z",
    "updatedAt": "2026-05-16T10:00:02Z",
    "completedAt": null
  }
]
```

### `POST /downloads`

Create a new download.

Example:

```bash
curl -X POST \
  -H "Authorization: Bearer $QG_TOKEN" \
  -H "Content-Type: application/json" \
  http://127.0.0.1:19329/downloads \
  -d '{
    "url": "https://example.com/file.iso",
    "outputPath": "C:/Downloads/file.iso",
    "connections": 8,
    "queueMode": false,
    "headers": {"Accept": "*/*"},
    "autoBuffer": true,
    "http1": false
  }'
```

Response (`201 Created`):

```json
{
  "id": "d_abc123",
  "url": "https://example.com/file.iso",
  "outputPath": "C:/Downloads/file.iso",
  "status": "queued",
  "downloaded": 0,
  "total": 0,
  "percent": 0,
  "speedMBps": 0,
  "avgMBps": 0,
  "error": "",
  "message": "download created",
  "createdAt": "2026-05-16T10:00:00Z",
  "updatedAt": "2026-05-16T10:00:00Z",
  "completedAt": null
}
```

### `GET /downloads/{id}`

Get one download job by ID.

Example:

```bash
curl -H "Authorization: Bearer $QG_TOKEN" \
  http://127.0.0.1:19329/downloads/d_abc123
```

Response:

```json
{
  "id": "d_abc123",
  "url": "https://example.com/file.iso",
  "outputPath": "C:/Downloads/file.iso",
  "status": "running",
  "downloaded": 2097152,
  "total": 734003200,
  "percent": 0.29,
  "speedMBps": 13.1,
  "avgMBps": 11.2,
  "error": "",
  "message": "downloading",
  "createdAt": "2026-05-16T10:00:00Z",
  "updatedAt": "2026-05-16T10:00:04Z",
  "completedAt": null
}
```

### `POST /downloads/{id}/pause`

Pause a running job.

```bash
curl -X POST \
  -H "Authorization: Bearer $QG_TOKEN" \
  http://127.0.0.1:19329/downloads/d_abc123/pause
```

### `POST /downloads/{id}/resume`

Resume a paused job.

```bash
curl -X POST \
  -H "Authorization: Bearer $QG_TOKEN" \
  http://127.0.0.1:19329/downloads/d_abc123/resume
```

### `POST /downloads/{id}/cancel`

Cancel a job.

```bash
curl -X POST \
  -H "Authorization: Bearer $QG_TOKEN" \
  http://127.0.0.1:19329/downloads/d_abc123/cancel
```

### `POST /downloads/{id}/delete`

Delete a job. Optionally delete downloaded files.

Without body (default `delete_files=false`):

```bash
curl -X POST \
  -H "Authorization: Bearer $QG_TOKEN" \
  http://127.0.0.1:19329/downloads/d_abc123/delete
```

With file deletion:

```bash
curl -X POST \
  -H "Authorization: Bearer $QG_TOKEN" \
  -H "Content-Type: application/json" \
  http://127.0.0.1:19329/downloads/d_abc123/delete \
  -d '{"delete_files": true}'
```

Response:

```json
{
  "ok": true,
  "id": "d_abc123",
  "delete_files": true
}
```

### `GET /events`

SSE stream of agent/download events.

Example:

```bash
curl -N -H "Authorization: Bearer $QG_TOKEN" \
  http://127.0.0.1:19329/events
```

Server sends `text/event-stream` frames:

```text
event: download.progress
data: {"type":"download.progress","id":"d_abc123","timestamp":"2026-05-16T10:00:05Z","downloaded":3145728,"total":734003200,"percent":0.43,"speedMBps":12.9,"avgMBps":11.5,"status":"running","message":"downloading","error":"","suggestion":""}
```

## SSE event examples

The stream can emit events including:

- `download.created`
- `download.started`
- `download.progress`
- `download.warning`
- `download.completed`
- `download.failed`

Examples:

```text
event: download.created
data: {"type":"download.created","id":"d_abc123","timestamp":"2026-05-16T10:00:00Z","status":"queued","message":"download created"}

event: download.started
data: {"type":"download.started","id":"d_abc123","timestamp":"2026-05-16T10:00:01Z","status":"running","message":"download started"}

event: download.progress
data: {"type":"download.progress","id":"d_abc123","timestamp":"2026-05-16T10:00:05Z","downloaded":3145728,"total":734003200,"percent":0.43,"speedMBps":12.9,"avgMBps":11.5,"status":"running","message":"downloading"}

event: download.warning
data: {"type":"download.warning","id":"d_abc123","timestamp":"2026-05-16T10:00:06Z","status":"running","message":"server may reject aggressive parallel downloads; try lowering -n","suggestion":"try lowering connections to 2 or 4"}

event: download.completed
data: {"type":"download.completed","id":"d_abc123","timestamp":"2026-05-16T10:01:10Z","downloaded":734003200,"total":734003200,"percent":100,"status":"completed","message":"download completed"}

event: download.failed
data: {"type":"download.failed","id":"d_abc124","timestamp":"2026-05-16T10:02:00Z","status":"failed","message":"download failed","error":"server returned 416"}
```

## Desktop integration notes

- Desktop app should keep a persistent connection to `GET /events` and drive UI updates from streamed events.
- Desktop app should invoke `POST` action endpoints (`/pause`, `/resume`, `/cancel`, `/delete`) for user actions.
- For close-to-tray behavior, closing the window should not stop the background agent; only explicit user quit should stop it.
