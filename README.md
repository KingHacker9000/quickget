# QuickGet

<p align="center">
  <strong>High-speed, resumable, multi-connection downloader for the command line.</strong><br/>
  Built in Go for reliable large-file downloads with smart recovery and tuning controls.
</p>

<p align="center">
  <code>Parallel ranges</code> • <code>Resume-safe manifests</code> • <code>Dynamic splitting</code> • <code>Disk-aware buffering</code>
</p>

---

## Why QuickGet

QuickGet is designed for one job: pull large files quickly without losing progress.

It combines parallel HTTP range downloads, resume metadata, and practical transport tuning so interrupted downloads can continue safely and efficiently.

## Highlights

- Parallel segmented downloading with HTTP range requests
- Resume support via `<output>.quickget.json` manifest files
- Dynamic work stealing / chunk splitting for better worker utilization
- Queue mode for fixed-size segment scheduling
- Custom request headers with repeatable `-H` flags
- Configurable `User-Agent` via `-user-agent`
- Automatic fallback to single-connection mode when ranges are unavailable
- Retry handling for transient network failures
- Live progress output with optional verbose per-chunk details
- Utility commands for URL inspection, manifest status, cleanup, hashing, and disk tuning

## Install

### Build from source

```bash
go build -o quickget.exe ./cmd/quickget
```

### Optional: run without building

```bash
go run ./cmd/quickget --help
```

## Quick Start

```bash
# Basic download (auto filename, Downloads directory)
quickget.exe download https://example.com/big.iso

# Pick filename and worker count
quickget.exe download -o big.iso -n 12 https://example.com/big.iso

# Legacy form (same as `download` command)
quickget.exe -o big.iso -retries 5 https://example.com/big.iso
```

## Commands

```text
quickget.exe download [options] <url>
quickget.exe inspect <url>
quickget.exe server-test <url>
quickget.exe status <output-file>
quickget.exe clean <output-file>
quickget.exe hash <file>
quickget.exe disk-test -o <temp-test-file>
quickget.exe tune-disk -o <temp-test-file>
```

## Project Structure

```text
cmd/quickget/           # executable entrypoint
pkg/quickget/cli/       # command parsing and terminal output
pkg/quickget/core/      # downloader engine and orchestration
pkg/quickget/manifest/  # resume-manifest data model and helpers
pkg/quickget/probe/     # inspect/server capability probing
pkg/quickget/tune/      # disk buffer tuning
pkg/quickget/hash/      # file hashing helpers
pkg/quickget/           # library facade for embedding in other Go projects
```

## GUI/Submodule Integration (Go)

QuickGet can be consumed directly as a Go module from a GUI project:

```go
import (
    "context"
    "quickget/pkg/quickget"
    "quickget/pkg/quickget/core"
)

opts := core.DefaultRequest()
opts.URL = "https://example.com/file.iso"
opts.OutputDir = "downloads"
opts.Workers = 8

_, err := quickget.Download(context.Background(), quickget.DownloadRequest{
    Options: opts,
})
if err != nil {
    // handle error
}
```

## Download Options

| Flag | Description | Default |
|---|---|---|
| `-o string` | Output filename (optional; auto-detected if omitted) | auto |
| `-dir string` | Target directory | OS Downloads folder |
| `-n int` | Parallel connections | `8` |
| `-retries int` | Max retries per segment/chunk | `3` |
| `-v` | Verbose per-chunk progress output | `false` |
| `-d` | Enable dynamic splitting/work stealing | `true` |
| `-min-split-size int` | Minimum bytes before a range can be split dynamically | `33554432` |
| `-min-dynamic-file-size int` | Minimum file size required to allow dynamic splitting | `67108864` |
| `-queue-mode` | Use queue-based fixed segment processing | `false` |
| `-segment-size int` | Segment size (bytes) for queue mode | `16777216` |
| `-buffer-size int` | Read/write buffer size in bytes | `1048576` |
| `-auto-buffer` | Auto-tune buffer size for target disk | `false` |
| `-http1` | Force HTTP/1.1 (disable HTTP/2) | `true` |
| `-max-idle-conns int` | Global max idle HTTP connections | `1024` |
| `-idle-timeout int` | Idle connection timeout (seconds) | `90` |
| `-write-disk string` | Only measure write stats for this disk/volume (example: `C:`) | empty |
| `-user-agent string` | User-Agent header value | `QuickGet/1.0` |
| `-H value` | Custom HTTP header, repeatable (example: `-H "Authorization: Bearer TOKEN"`) | none |

QuickGet accepts download flags before or after the URL; argument order is normalized internally.

## Command Behavior

- `inspect <url>`: Sends a HEAD request and reports final URL, status, content length, and range support.
- `server-test <url>`: Runs HTTP capability checks against the target server.
- `status <output-file>`: Reads `<output-file>.quickget.json`, prints a summary, then raw manifest JSON.
- `clean <output-file>`:
  - Deletes `<output-file>.quickget.json`.
  - Deletes `<output-file>` only if manifest indicates incomplete download.
  - Leaves output file untouched when no manifest exists.
- `hash <file>`: Prints SHA-256 as `<hex>  <file>`.
- `disk-test` / `tune-disk`: Runs disk write timing to guide buffer tuning.

## Resume Model

QuickGet stores progress in two files:

```text
<output file>
<output file>.quickget.json
```

If interrupted, rerun the same command to continue. Remove the `.quickget.json` file to reset state.

## Practical Recipes

```bash
# High-throughput dynamic mode
quickget.exe download -o sample.dat -n 16 -d https://proof.ovh.net/files/1Gb.dat

# Queue-mode download with larger segment size
quickget.exe download -o sample.dat -n 12 -queue-mode -segment-size 33554432 https://proof.ovh.net/files/1Gb.dat

# Auto-tune buffer and run with verbose output
quickget.exe download -o sample.dat -n 8 -auto-buffer -v https://proof.ovh.net/files/1Gb.dat

# Send custom User-Agent and custom headers
quickget.exe download -o private.bin -user-agent "Mozilla/5.0 QuickGet" -H "Authorization: Bearer TOKEN" -H "X-Client: QuickGet" https://example.com/private.bin

# Verify server and inspect metadata first
quickget.exe server-test https://proof.ovh.net/files/1Gb.dat
quickget.exe inspect https://proof.ovh.net/files/1Gb.dat
```

## Benchmarking

```bash
# Parameter grid search benchmark
python .temp/benchmark_search.py --repeats 2 --top 10

# Custom benchmark script
python .temp/benchmark.py
```

## Notes

- Parallel mode requires server range support and known file size.
- When range support is unavailable, QuickGet falls back to single-stream mode automatically.
- `-http1` defaults to `true` in current builds.
- If `User-Agent` is not provided in headers, QuickGet applies `-user-agent` automatically.

---

Built for fast, restart-safe downloads with explicit control over throughput behavior.
