# Desktop Engine Contract

## 1) Chosen architecture

QuickGet desktop is split into:

- **Tauri desktop app** (UI shell and desktop integrations)
- **`quickget-agent` sidecar/background process** (download engine API)

The desktop app is a client of the local agent API (`http://127.0.0.1:19329` by default), not the download engine itself.

## 2) Responsibilities

### `quickget-agent` owns

- Download queue management
- Active job lifecycle tracking
- Progress/state emission
- Pause/resume/cancel operations
- Persisted and recoverable job state
- Error diagnosis and actionable suggestions
- Direct access to QuickGet download engine internals

### Desktop app owns

- User interface and interaction flows
- System tray behavior and window visibility
- Output location picker UX
- Settings UX and persistence (app-level preferences)
- Desktop notifications
- Calling the agent HTTP/SSE API

## 3) Lifecycle contract

- On desktop app startup, app checks if `quickget-agent` is reachable.
- If agent is not running, desktop app launches `quickget-agent serve`.
- Closing the main window hides to tray; it does **not** stop agent by default.
- Downloads continue while tray app remains alive.
- On explicit app quit, desktop app prompts the user to choose behavior for active downloads (recommended: pause active downloads before shutdown).

## 4) API usage contract

Desktop app uses the local agent API as follows:

- Health and readiness:
  - `GET /health` (no auth)
- Downloads:
  - `GET /downloads`
  - `POST /downloads`
  - `GET /downloads/{id}`
  - `POST /downloads/{id}/pause`
  - `POST /downloads/{id}/resume`
  - `POST /downloads/{id}/cancel`
  - `POST /downloads/{id}/delete` (optional `{"delete_files": true}`)
- Browser captures:
  - `GET /captures`
  - `POST /captures`
  - `GET /captures/{id}`
  - `POST /captures/{id}/reject`
  - `POST /captures/{id}/start`
- Profiler:
  - `GET /profiler`
  - `POST /profiler/run`
  - `POST /profiler/cancel`
- Event stream:
  - `GET /events` (SSE stream)

The desktop app should treat SSE events as the source of truth for rendering real-time state transitions.

## 5) Agent feature contract (authoritative behaviors)

### Downloads

- Queued and running job lifecycle with persisted state across agent restart.
- Pause, resume, cancel, delete controls.
- Progress snapshots include bytes, percent, speed, segments, and status metadata.
- Create-download options supported by engine:
  - URL, output path, directory
  - connections, retries
  - queue mode + segment size
  - buffer size + auto buffer
  - force HTTP/1.1
  - custom headers + user-agent

### Browser captures

- Capture ingestion API for browser-originated download requests.
- Duplicate detection metadata for capture decisions.
- Explicit reject path.
- Start-from-capture path with duplicate policy:
  - `overwrite`
  - `new_name`
  - `show_existing` (informational; no start)

### Profiler

- Agent-managed benchmark/profile orchestration.
- Run request supports:
  - `level` (`quick|normal|exhaustive`)
  - `sizes` (CSV subset of `10MB,100MB,1GB`)
  - `repeats`
  - `url`
- Produces recommendation payload and artifact references (`raw_results.csv`, `summary.csv`, profile directory).

## 6) CLI surface contract for agents/operators

The CLI must remain a complete operator surface over the local agent:

- `quickget agent health`
- `quickget agent list`
- `quickget agent get <id>`
- `quickget agent download <url> [options]`
- `quickget agent pause <id>`
- `quickget agent resume <id>`
- `quickget agent cancel <id>`
- `quickget agent delete <id> [-delete-files]`
- `quickget agent captures list|get|reject|start ...`
- `quickget agent profiler status|run|cancel ...`

All non-health operations require loading bearer token from user config path (`QuickGet/agent-token`) unless explicitly provided by another trusted path in future changes.

## 7) Error UX contract

Desktop UI must present user-friendly messages derived from agent diagnostics and suggestions.

Required UX examples:

- **Server rate limited**: automatically lower connections and inform user.
- **Range unsupported**: fallback to single connection and explain slower speed.
- **Disk bottleneck**: suggest SSD or different output folder.
- **Network failure**: offer retry now or resume later.

Error copy should prioritize clear action over raw internal error text.

## 8) Security contract

- Agent bind target is localhost/loopback only.
- Agent API requires bearer token auth for non-health routes.
- Desktop app must never log `Authorization` header values.
- Desktop app must never log custom secrets (for example sensitive request headers).

## 9) Future Chrome extension contract

Planned integration path:

- Chrome extension communicates with a native host bridge.
- Native host forwards download URLs/commands to `quickget-agent`.
- Extension does not implement downloading itself; `quickget-agent` remains the single download engine authority.

This keeps queue/state/retry behavior consistent across desktop UI and extension-triggered flows.
