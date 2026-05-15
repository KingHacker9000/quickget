# QuickGet

<!-- Replace OWNER with your GitHub username or org, e.g. ashishajin/quickget -->
[![CI](https://github.com/OWNER/quickget/actions/workflows/ci.yml/badge.svg)](https://github.com/OWNER/quickget/actions/workflows/ci.yml)
[![CodeQL](https://github.com/OWNER/quickget/actions/workflows/codeql.yml/badge.svg)](https://github.com/OWNER/quickget/actions/workflows/codeql.yml)
[![Release](https://img.shields.io/github/v/release/OWNER/quickget)](https://github.com/OWNER/quickget/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/OWNER/quickget)](go.mod)

<div align="center">

**Fast, resumable multi-connection downloads for the terminal**

Built in Go with parallel range requests, resume-safe manifests, and practical tuning controls for large files.

</div>

## Features

- Parallel segmented downloads with HTTP range requests
- Reliable resume with manifest state (`.quickget.json`)
- Dynamic splitting (work stealing) for improved worker utilization
- Queue mode with fixed-size segments
- Automatic fallback to single-stream mode when ranges are unavailable
- Retry handling for transient network failures
- Custom headers (`-H`) and configurable `User-Agent`
- Utilities for probing, status, cleanup, hashing, and disk tuning

## Installation

### Build

```bash
go build -o quickget.exe ./cmd/quickget
```

### Run without building

```bash
go run ./cmd/quickget --help
```

## Quick Start

```bash
# Basic download
quickget.exe download https://example.com/file.iso

# Specify output file and worker count
quickget.exe download -o file.iso -n 12 https://example.com/file.iso

# Legacy shorthand (equivalent to download command)
quickget.exe -o file.iso -retries 5 https://example.com/file.iso
```

## CLI Commands

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

## Download Flags

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
| `-write-disk string` | Measure write stats only for this disk/volume (example: `C:`) | empty |
| `-user-agent string` | User-Agent header value | `QuickGet/1.0` |
| `-H value` | Custom HTTP header (repeatable) | none |

## Examples

```bash
# Dynamic mode with higher parallelism
quickget.exe download -o sample.dat -n 16 -d https://proof.ovh.net/files/1Gb.dat

# Queue mode with larger fixed segment size
quickget.exe download -o sample.dat -n 12 -queue-mode -segment-size 33554432 https://proof.ovh.net/files/1Gb.dat

# Auto-tune buffer size and print verbose progress
quickget.exe download -o sample.dat -n 8 -auto-buffer -v https://proof.ovh.net/files/1Gb.dat

# Custom User-Agent and headers
quickget.exe download -o private.bin -user-agent "Mozilla/5.0 QuickGet" -H "Authorization: Bearer TOKEN" -H "X-Client: QuickGet" https://example.com/private.bin
```

## Resume Behavior

QuickGet persists progress using:

```text
<output-file>
<output-file>.quickget.json
```

If a download is interrupted, rerun the same command to continue. Delete the manifest file to reset progress.

## Repository Structure

```text
FastGet/
|-- cmd/
|   `-- quickget/
|       `-- main.go                 # CLI entrypoint
|-- pkg/
|   `-- quickget/
|       |-- quickget.go             # Public package facade
|       |-- cli/
|       |   |-- cli.go              # Command parsing and CLI dispatch
|       |   `-- cli_test.go
|       |-- core/
|       |   `-- downloader.go       # Download engine and orchestration
|       |-- hash/
|       |   `-- hash.go             # File hashing helpers
|       |-- manifest/
|       |   |-- manifest.go         # Resume manifest model and I/O
|       |   `-- manifest_test.go
|       |-- probe/
|       |   |-- probe.go            # URL/server probing utilities
|       |   `-- probe_test.go
|       |-- progress/
|       |   `-- progress.go         # Progress reporting helpers
|       `-- tune/
|           `-- tune.go             # Disk/buffer tuning logic
|-- .temp/                          # Local scripts and benchmark tooling
|-- downloads/                      # Local test output artifacts
|-- go.mod
|-- LICENSE
`-- README.md
```

## Development

```bash
# Run unit tests
go test ./...

# Benchmark helpers (local tooling)
python .temp/benchmark_search.py --repeats 2 --top 10
python .temp/benchmark.py
```

## Notes

- Range-based parallelism depends on server support and known file size.
- If range requests are unsupported, QuickGet automatically falls back to single-stream mode.
- If `User-Agent` is not provided in custom headers, QuickGet applies `-user-agent`.

## License

Licensed under the MIT License. See `LICENSE`.
