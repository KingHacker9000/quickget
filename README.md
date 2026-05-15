# ⚡ FastGet

<div align="center">

**A fast, resumable multi-connection command-line downloader written in Go.**

FastGet uses parallel HTTP range requests, byte-level resume support, and dynamic work stealing to maximize download throughput while remaining lightweight and dependency-free.

</div>

---

## ✨ Features

- 🚀 Parallel segmented downloading
- 🔄 Reliable byte-level resume support
- 🧠 Dynamic chunk splitting / work stealing
- 📦 Preallocated output files for stable random writes
- ⚠️ Automatic fallback when HTTP range requests are unsupported
- 🔁 Retry support for transient network failures
- 📊 Real-time progress display with optional verbose chunk details
- 🪶 Lightweight single-binary implementation
- 🛠️ No external runtime dependencies

---

## 🖥️ Supported Platforms

- Windows
- macOS
- Linux

---

# 📥 Installation

## Build From Source

### Windows

```bash
go build -o fastget.exe
```

### macOS / Linux

```bash
go build -o fastget
```

---

# 🚀 Usage

```bash
fastget.exe [options] <url>
```

---

# ⚙️ Options

| Flag | Description | Default |
|---|---|---|
| `-o string` | Output filename | Required |
| `-n int` | Parallel connections | `8` |
| `-retries int` | Max retries per segment | `3` |
| `-v` | Verbose progress output | `false` |
| `-d` | Enable dynamic chunk splitting/work stealing | `true` |
| `-min-split-size int` | Minimum remaining range size before splitting | `8388608` (8 MB) |
| `-min-dynamic-file-size int` | Minimum file size required for dynamic splitting | `67108864` (64 MB) |

---

# ⚡ Quick Examples

## Standard Download

```bash
fastget.exe -o sample.dat https://proof.ovh.net/files/100Mb.dat
```

---

## Increase Parallel Connections

```bash
fastget.exe -o sample.dat -n 12 https://proof.ovh.net/files/100Mb.dat
```

---

## Increase Retry Count

```bash
fastget.exe -o sample.dat -n 12 -retries 5 https://proof.ovh.net/files/100Mb.dat
```

---

## Disable Dynamic Splitting

```bash
fastget.exe -o sample.dat -d=false https://proof.ovh.net/files/100Mb.dat
```

---

## Verbose Chunk Debugging

```bash
fastget.exe -o sample.dat -v https://proof.ovh.net/files/100Mb.dat
```

---

# 🔄 Resume Support

FastGet automatically stores download state using manifest files.

Example:

```text
sample.dat
sample.dat.fastget.json
```

If the download is interrupted:

```bash
fastget.exe -o sample.dat <url>
```

FastGet resumes automatically from completed byte ranges.

---

# 🧠 Dynamic Chunk Splitting

FastGet supports optional dynamic work stealing.

When enabled:
- Faster workers automatically help slower workers
- Large unfinished ranges can be split dynamically
- Improves throughput on unstable or uneven connections

Dynamic splitting only activates when:

- `-d=true`
- File size exceeds `-min-dynamic-file-size`
- Remaining ranges exceed `-min-split-size`

This avoids unnecessary scheduling overhead on small downloads.

---

# 📊 Benchmarking

Local benchmark helpers are included in `.temp/`.

### Grid Search Benchmark

```bash
python .temp/benchmark_search.py --repeats 2 --top 10
```

### Custom Benchmarking

```bash
python .temp/benchmark.py
```

---

# 🛠️ Tuning Tips

## Increase `-n` when:
- Network bandwidth is high
- Latency is low
- The server allows many parallel connections

---

## Lower `-n` when:
- The server throttles aggressively
- Disk or network becomes saturated
- Small files perform worse with high parallelism

---

## Dynamic Splitting

Smaller `-min-split-size`:
- More aggressive balancing
- More scheduler overhead

Larger `-min-split-size`:
- Less overhead
- Fewer work-stealing operations

---

# 📁 Project Structure

```text
.
├── main.go
├── go.mod
├── downloads/
└── .temp/
    ├── benchmark.py
    └── benchmark_search.py
```

---

# 🧩 How It Works

1. FastGet checks whether the server supports HTTP range requests.
2. The file is divided into segments.
3. Workers download segments concurrently.
4. Progress is tracked in a manifest file.
5. Interrupted downloads resume from saved byte ranges.
6. Optional work stealing redistributes slow remaining ranges dynamically.

---

# ⚠️ Notes

- `-o` is required.
- If HTTP range requests are unsupported, FastGet automatically falls back to single-connection mode.
- Deleting the `.fastget.json` manifest removes resume state.
- Dynamic splitting is disabled automatically for small files.

---

<div align="center">

### ⚡ Fast • Resumable • Parallel • Lightweight

</div>