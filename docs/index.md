# FastGet Documentation

FastGet is the repository, and `quickget` is the CLI binary. It is a resumable, parallel downloader built in Go for large files and unstable networks.

## What You Can Do With QuickGet

- Download with multiple range workers for higher throughput.
- Resume interrupted downloads with a manifest file.
- Switch between dynamic splitting and queue mode.
- Inspect server capabilities before downloading.
- Tune buffering and disk strategy for your machine.

## Who This Documentation Is For

- New users who want a clean first download in minutes.
- Power users tuning throughput and reliability.
- Contributors who want internals, tests, and extension points.

## Start Here

1. Read [Getting Started](getting-started.md) for install and your first commands.
2. Use [CLI Reference](cli-reference.md) for full command/flag details.
3. Explore [Use Cases](use-cases.md) for practical workflows.
4. Go to [Deep Dive](deep-dive/architecture.md) for internals.

## Project Snapshot

- Language: Go
- CLI entrypoint: `cmd/quickget/main.go`
- Core package: `pkg/quickget/core`
- Resume manifest: `<output-file>.quickget.json`
- License: MIT

## Safety Notes

- Only download files from trusted sources.
- Validate checksums when the source provides them.
- Auth headers passed via `-H` may appear in shell history; use secure shell practices.
