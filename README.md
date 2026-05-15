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

## Build

```bash
go build -o fastget.exe
```

## Usage

```bash
fastget.exe [options] <url>
```

## Options

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

## Examples

```bash
fastget.exe -o sample.dat https://proof.ovh.net/files/100Mb.dat
fastget.exe -o sample.dat -n 12 -retries 5 https://proof.ovh.net/files/100Mb.dat
fastget.exe -o sample.dat -d=false https://proof.ovh.net/files/100Mb.dat
fastget.exe -o sample.dat -http1 https://proof.ovh.net/files/100Mb.dat
fastget.exe -o sample.dat -max-idle-conns 2048 -idle-timeout 120 https://proof.ovh.net/files/100Mb.dat
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
