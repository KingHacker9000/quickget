# Deep Dive: Architecture

## Package Layout

- `cmd/quickget/main.go`: process entrypoint.
- `pkg/quickget/cli`: command routing, flag parsing, usage output.
- `pkg/quickget/core`: download orchestration, worker scheduling, HTTP client setup.
- `pkg/quickget/probe`: URL validation, HEAD metadata, range probing, filename detection.
- `pkg/quickget/manifest`: resume state model, range normalization, totals.
- `pkg/quickget/progress`: progress loops and write-time stats.
- `pkg/quickget/tune`: disk test and buffer recommendation.
- `pkg/quickget/hash`: file SHA-256 helper.
- `pkg/quickget/quickget.go`: exported facade for library-style usage.

## Control Flow

```text
CLI args
  -> cli.Run
  -> parse + validate options
  -> quickget.Download
  -> core.Download
  -> probe.FetchURLInfo (HEAD)
  -> mode decision (single / parallel)
  -> transfer engine
  -> manifest finalize + cleanup
```

## Download Mode Decision

Single mode is forced when any condition is true:

- Unknown file size (`Content-Length` absent/invalid).
- Range not supported by server.
- Worker count is `1`.

Parallel mode is used otherwise.

## Parallel Internals

- Preallocates file for known total size.
- Initializes or loads `.quickget.json` manifest.
- Tracks completed byte ranges per chunk.
- Saves manifest periodically and after each completed task.
- Supports two schedulers:
  - Dynamic splitting scheduler (range stealing).
  - Queue-mode fixed segment scheduler.

## Failure and Recovery Model

- Segment/chunk task retries up to `-retries`.
- Manifest save loop persists state every few seconds.
- On Ctrl+C/SIGTERM, state is saved before process exit.
- Re-running same command resumes missing ranges.

## HTTP Behavior

- Configurable transport pool via `-max-idle-conns` and `-idle-timeout`.
- `-http1` true by default to force HTTP/1.1 behavior.
- If no `User-Agent` header is provided in `-H`, `-user-agent` is applied.
