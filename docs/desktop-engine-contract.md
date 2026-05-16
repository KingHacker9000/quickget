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

- Create a download: `POST /downloads`
- Receive live state/progress: `GET /events` (SSE stream)
- Control jobs:
  - `POST /downloads/{id}/pause`
  - `POST /downloads/{id}/resume`
  - `POST /downloads/{id}/cancel`

The desktop app should treat SSE events as the source of truth for rendering real-time state transitions.

## 5) Error UX contract

Desktop UI must present user-friendly messages derived from agent diagnostics and suggestions.

Required UX examples:

- **Server rate limited**: automatically lower connections and inform user.
- **Range unsupported**: fallback to single connection and explain slower speed.
- **Disk bottleneck**: suggest SSD or different output folder.
- **Network failure**: offer retry now or resume later.

Error copy should prioritize clear action over raw internal error text.

## 6) Security contract

- Agent bind target is localhost/loopback only.
- Agent API requires bearer token auth for non-health routes.
- Desktop app must never log `Authorization` header values.
- Desktop app must never log custom secrets (for example sensitive request headers).

## 7) Future Chrome extension contract

Planned integration path:

- Chrome extension communicates with a native host bridge.
- Native host forwards download URLs/commands to `quickget-agent`.
- Extension does not implement downloading itself; `quickget-agent` remains the single download engine authority.

This keeps queue/state/retry behavior consistent across desktop UI and extension-triggered flows.
