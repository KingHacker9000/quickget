# Deep Dive: Performance Tuning

This page explains how to tune QuickGet for speed and stability.

## Core Levers

## `-n` Parallel Connections

- Start with `8`.
- Increase to `12-20` only if server supports ranges and does not throttle.
- Lower to `1-4` when you see `429`, `403`, `503`, or repeated failures.

## `-d` Dynamic Splitting

- Enabled by default.
- Best for large files with uneven worker completion.
- Disabled automatically for small files below `-min-dynamic-file-size`.

## `-queue-mode` + `-segment-size`

- Use for predictable fixed-size work units.
- Good for repeatable benchmarking.
- Larger segment size reduces scheduler overhead but may reduce balancing.

## Buffering

## `-buffer-size`

- Default is `1 MiB`.
- Larger buffers can improve throughput on fast disks, but use more memory.

## `-auto-buffer`

- Runs disk recommendation logic and picks a tested buffer size.
- Ignored if `-buffer-size` is explicitly set.

## Transport and Protocol

## `-http1`

- Default true.
- Keeps behavior predictable across servers that do not handle parallel HTTP/2 streams well.

## `-max-idle-conns` and `-idle-timeout`

- Increase only when running many downloads or very high `-n`.
- Defaults are already high enough for most single-download use.

## Practical Tuning Recipes

## High-bandwidth trusted server

```bash
quickget.exe download -o file.bin -n 16 -d -retries 3 https://example.com/file.bin
```

## Rate-limited server

```bash
quickget.exe download -o file.bin -n 2 -retries 8 https://example.com/file.bin
```

## Disk-sensitive environment

```bash
quickget.exe disk-test -o C:\temp\qg-disk.bin
quickget.exe download -o file.bin -auto-buffer -n 8 https://example.com/file.bin
```

## Benchmarking Guidance

Use `profile` for repeatable benchmark runs:

```bash
quickget.exe profile --level normal --sizes 10MB,100MB,1GB --repeats 3
```

- `quick` level: faster coarse search.
- `normal` level: balanced staged tournament.
- `exhaustive` level: full queue-mode matrix sweep.
- `--url` can pin benchmarking to your own endpoint.

Profile artifacts are saved under `.quickget/profiles/<timestamp>/`:

- `raw_results.csv` for per-run details.
- `summary.csv` for ranked aggregates.
- `recommendations.json` for best settings per size.

For manual testing outside `profile`, keep URL and size fixed, change one variable at a time, and compare retries plus throughput.
