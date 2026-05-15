# FastGet

A fast, resumable multi-connection command-line downloader written in Go.

## Features

- Parallel segmented downloading (HTTP range requests)
- Resume support with byte-range manifests
- Dynamic chunk splitting / work stealing
- Shared HTTP client with transport tuning
- Automatic single-connection fallback when ranges are unsupported
- Retry support for transient network failures
- Real-time progress output with optional per-chunk verbosity
- URL inspection, resume status, cleanup, and SHA-256 hashing commands

## Build

```bash
go build -o fastget.exe
```

## Usage

```bash
fastget.exe download [options] <url>
fastget.exe inspect <url>
fastget.exe status <output-file>
fastget.exe clean <output-file>
fastget.exe hash <file>
```

Backward compatibility:

```bash
fastget.exe [options] <url>
```

This behaves the same as `fastget.exe download [options] <url>`.

## Download Options

| Flag | Description | Default |
|---|---|---|
| `-o string` | Output filename | Required |
| `-n int` | Parallel connections | `8` |
| `-retries int` | Max retries per segment | `3` |
| `-v` | Verbose per-chunk progress | `false` |
| `-d` | Enable dynamic splitting/work stealing | `true` |
| `-min-split-size int` | Min remaining range size before dynamic split (bytes) | `8388608` |
| `-min-dynamic-file-size int` | Min file size to allow dynamic splitting (bytes) | `67108864` |
| `-http1` | Disable HTTP/2 and force HTTP/1.1 behavior | `false` |
| `-max-idle-conns int` | Max idle connections globally | `1024` |
| `-idle-timeout int` | Idle connection timeout (seconds) | `90` |

Download flags can appear before or after the URL; FastGet normalizes args and treats both styles equivalently.

## Command Behavior

- `inspect <url>`: Performs a HEAD request and prints input URL, final redirected URL, HTTP status, content length (or unknown), and range support.
- `status <output-file>`: Reads `<output-file>.fastget.json`, prints a human summary, then prints raw manifest JSON.
- `clean <output-file>`:
  - Removes `<output-file>.fastget.json`.
  - Removes `<output-file>` only when the manifest indicates an incomplete download.
  - If no manifest exists, the output file is left unchanged.
- `hash <file>`: Streams file contents and prints SHA-256 as `<hex>  <file>`.

## Examples

```bash
fastget.exe download -o sample.dat https://proof.ovh.net/files/100Mb.dat
fastget.exe download https://proof.ovh.net/files/100Mb.dat -o sample.dat -n 12
fastget.exe -o sample.dat -retries 5 https://proof.ovh.net/files/100Mb.dat
fastget.exe inspect https://proof.ovh.net/files/100Mb.dat
fastget.exe status sample.dat
fastget.exe clean sample.dat
fastget.exe hash sample.dat
```

## Benchmarking

Grid search benchmark:

```bash
python .temp/benchmark_search.py --repeats 2 --top 10
```

Custom benchmark:

```bash
python .temp/benchmark.py
```

## Resume Files

FastGet stores state in:

```text
<output file>
<output file>.fastget.json
```

Delete the `.fastget.json` file to reset resume state.