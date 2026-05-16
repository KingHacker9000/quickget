# Native Messaging Plan (Future)

## Goal

Define a future Chrome native messaging bridge for QuickGet without implementing it yet.

## End-to-end flow

1. Chrome extension captures (or receives) a download URL.
2. Extension sends a native messaging request to `quickget-native-host`.
3. `quickget-native-host` validates and transforms the request.
4. Native host forwards the request to `quickget-agent` over localhost HTTP API.
5. `quickget-agent` enqueues and manages the download lifecycle.

## Ownership model

### `quickget-agent` (system of record)

- Owns queue state
- Owns active download execution
- Owns pause/resume/cancel semantics
- Owns progress/events/state persistence
- Owns all download-engine behavior and diagnostics

### `quickget-native-host` (thin bridge)

- Receives native messages from extension
- Performs minimal validation/translation
- Calls local agent API via `pkg/quickget/agentclient`
- Returns compact success/error responses to extension

`quickget-native-host` must remain a tiny transport bridge and must **not** implement downloading.

## Native host client requirement

The native host should use existing client code:

- `pkg/quickget/agentclient`

Reason:

- Keeps auth/header behavior consistent
- Reuses existing endpoint contract handling
- Avoids duplicate API wiring logic

## Message scope (initial)

Initial native messaging payload should support only the minimum required fields to create a download, such as:

- URL
- Optional output path/directory hint
- Optional safe header subset (if needed)

Bridge should forward only data required by `POST /downloads` and avoid pass-through of arbitrary fields by default.

## Installation and registration

Native messaging requires platform-specific manifest registration.

Planned installation responsibilities:

- Install `quickget-native-host` binary
- Write/register native messaging host manifest for Chrome
- Configure manifest path/location per platform:
  - Windows registry + manifest JSON path
  - macOS/Linux native messaging host manifest locations

This is deployment/installer work and is out of scope for current implementation.

## Security requirements

- Accept native messages only from the official QuickGet Chrome extension ID.
- Do not log sensitive data (for example auth headers, cookies, tokens).
- Forward only necessary fields to the agent API.
- Keep host-agent communication local (`localhost`/loopback only).
- Treat extension-provided headers/metadata as untrusted input and validate/sanitize before forwarding.

## Non-goals (for this phase)

- Implementing `quickget-native-host`
- Implementing Chrome extension code
- Defining installer/registry scripts
- Adding new download engine logic

## Future implementation sketch

When implementation starts, expected components are:

- `cmd/quickget-native-host` (native messaging stdin/stdout protocol)
- Request/response schema for extension-host messages
- Host-side adapter to `pkg/quickget/agentclient`
- Basic structured error mapping from agent -> extension
- Platform packaging hooks for host manifest registration
