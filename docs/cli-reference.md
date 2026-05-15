# CLI Reference

`quickget.exe` supports command form and legacy shorthand.

## Command Forms

```text
quickget.exe download [options] <url>
quickget.exe inspect <url>
quickget.exe filestats <url>
quickget.exe server-test <url>
quickget.exe status <output-file>
quickget.exe clean <output-file>
quickget.exe hash <file>
quickget.exe disk-test -o <temp-test-file>
quickget.exe tune-disk -o <temp-test-file>
```

Legacy shorthand:

```text
quickget.exe [options] <url>
```

Equivalent to `download`.

## Download Flags

| Flag | Type | Default | Description |
|---|---|---:|---|
| `-o` | string | auto | Output filename. |
| `-dir` | string | OS Downloads | Target directory. |
| `-n` | int | `8` | Parallel connections. |
| `-retries` | int | `3` | Max retries per chunk/segment. |
| `-v` | bool | `false` | Verbose progress with per-chunk status. |
| `-d` | bool | `true` | Dynamic splitting (work stealing). |
| `-min-split-size` | int64 | `33554432` | Minimum bytes needed to split an active range. |
| `-min-dynamic-file-size` | int64 | `67108864` | Minimum file size to allow dynamic splitting. |
| `-queue-mode` | bool | `false` | Fixed-size queue segments instead of dynamic ranges. |
| `-segment-size` | int64 | `16777216` | Segment size for queue mode. |
| `-buffer-size` | int | `1048576` | Read/write buffer size in bytes. |
| `-auto-buffer` | bool | `false` | Auto-select buffer size from disk tests. |
| `-http1` | bool | `true` | Force HTTP/1.1 (disables HTTP/2 attempts). |
| `-max-idle-conns` | int | `1024` | Global max idle connections. |
| `-idle-timeout` | int | `90` | Idle timeout in seconds. |
| `-write-disk` | string | empty | Restrict write-stat sampling to a volume (example `C:`). |
| `-user-agent` | string | `QuickGet/1.0` | User-Agent if none provided via headers. |
| `-H` | repeatable | none | Custom header, format `Header-Name: value`. |

## Command Details

## `download`

Downloads one URL. Chooses single-stream fallback automatically when range requests are unavailable or size is unknown.

```bash
quickget.exe download -o sample.dat -n 16 https://proof.ovh.net/files/1Gb.dat
```

## `inspect`

Runs HEAD and prints resolved URL, status, size, range support, and suggested filename.

```bash
quickget.exe inspect https://example.com/file.iso
```

## `filestats`

Prints remote metadata focused on range and content-disposition behavior.

```bash
quickget.exe filestats https://example.com/file.iso
```

## `server-test`

Performs a quick range probe (`bytes=0-0`) and prints a recommended connection strategy.

```bash
quickget.exe server-test https://example.com/file.iso
```

## `status`

Reads `<output>.quickget.json` and reports progress, chunk completion, mode, and raw JSON.

```bash
quickget.exe status file.iso
```

## `clean`

Removes manifest; removes partial output only when download is incomplete.

```bash
quickget.exe clean file.iso
```

## `hash`

Outputs SHA-256 in `sha256  filename` format.

```bash
quickget.exe hash file.iso
```

## `disk-test` / `tune-disk`

Runs repeated disk write tests for candidate buffer sizes and recommends one.

```bash
quickget.exe disk-test -o C:\temp\quickget-disk-test.bin
```

## Error Cases to Expect

- `invalid URL` when URL parsing fails.
- `exactly one positional URL argument is required` when missing or extra positionals are provided.
- Flag validation errors such as `-n must be greater than 0`.
- Header parsing errors when `-H` is not `name: value`.
