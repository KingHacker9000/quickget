# QuickGet Agent Architecture

## Purpose
`quickget-agent` is the background download controller for QuickGet.

It is responsible for:
- Owning the download queue lifecycle (queued, running, paused, completed, failed, canceled).
- Tracking active jobs and historical jobs.
- Managing user-facing state and user-facing error reporting.
- Invoking the existing QuickGet downloader/core packages under `pkg/quickget/` to execute actual download work.

The downloader engine remains the download executor. The agent is the orchestration layer around it.

## Process Model
QuickGet will operate as a multi-process local architecture:
- `quickget` CLI remains a direct command-line tool for immediate operations.
- `quickget-agent` is introduced as a separate long-running Go binary.
- A future Tauri desktop app will start (or connect to) `quickget-agent` and control downloads through its local API.
- A future Chrome native host integration will forward captured URLs/commands to `quickget-agent`.

This separation keeps the current CLI simple while enabling persistent background job management for UI-driven workflows.

## Proposed Repository Structure
Planned additions in this repo:

```text
cmd/quickget-agent/
pkg/quickget/agent/
pkg/quickget/api/
pkg/quickget/events/
pkg/quickget/store/
```

Intended responsibilities:
- `cmd/quickget-agent/`: binary entrypoint, config wiring, startup/shutdown.
- `pkg/quickget/agent/`: queue manager, job lifecycle, orchestration logic.
- `pkg/quickget/api/`: HTTP handlers, request/response contracts, auth middleware.
- `pkg/quickget/events/`: event model and event fan-out/streaming.
- `pkg/quickget/store/`: persistence for agent-level queue/history metadata.

## Agent API (Initial)
The initial local HTTP API surface:

- `GET /health`
- `POST /downloads`
- `GET /downloads`
- `GET /downloads/{id}`
- `POST /downloads/{id}/pause`
- `POST /downloads/{id}/resume`
- `POST /downloads/{id}/cancel`
- `POST /downloads/{id}/delete`
- `GET /events`

Behavioral intent:
- `POST /downloads` enqueues a new download request (not necessarily immediate start).
- `GET /downloads` returns queue + active + recent history summary.
- `GET /downloads/{id}` returns detailed state, progress, and terminal error (if any).
- Pause/resume/cancel/delete mutate download lifecycle according to current state.
- `/events` provides live progress and state transitions for clients.

## Event Stream
Initial streaming mechanism: **Server-Sent Events (SSE)** via `GET /events`.

Why SSE first:
- Agent-to-client communication is primarily one-way (progress updates, state changes, completion/failure notifications).
- SSE is simpler than WebSockets for this one-way pattern.
- SSE works well over standard HTTP, with straightforward reconnect behavior.
- It keeps implementation and operational complexity low for the first version.

WebSockets can be revisited later if bidirectional real-time interaction becomes necessary.

## State Distinction
Two state files serve different layers and must remain conceptually separate:

- `.quickget.json` manifest: downloader/engine resume state.
  - Purpose: support chunk-level resume and download continuity.
  - Scope: per-download engine internals.

- `agent-state.json`: agent queue/history/UI state.
  - Purpose: persist queue order, logical status, history entries, user-facing metadata, and user-facing errors.
  - Scope: orchestration-level state across downloads.

The agent should treat engine state as execution detail and maintain its own orchestration state independently.

## Security Model (Local-First)
Initial security requirements:
- Bind API listener only to `127.0.0.1`.
- Require a local auth token for all mutating endpoints (e.g., `POST /downloads`, pause/resume/cancel/delete).
- Never log `Authorization` headers.
- Never log private custom headers (for example token-bearing internal headers).

This provides a baseline local trust boundary while reducing accidental credential leakage in logs.

## Non-Goals (This Phase)
Out of scope for initial agent architecture/implementation:
- No desktop UI implementation yet.
- No Chrome extension implementation yet.
- No database adoption yet.
- No cloud sync.
- No rewrite of the existing downloader engine.

The first milestone is a local agent process that orchestrates the current downloader reliably.
