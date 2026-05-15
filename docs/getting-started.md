# Getting Started

## Install

### Option 1: Go install

```bash
go install github.com/KingHacker9000/quickget/cmd/quickget@latest
```

### Option 2: Build from source

```bash
go build -o quickget.exe ./cmd/quickget
```

### Option 3: Run without building

```bash
go run ./cmd/quickget --help
```

## First Download

```bash
quickget.exe download https://proof.ovh.net/files/100Mb.dat
```

If `-o` is omitted, QuickGet derives a filename from server metadata and saves to your OS Downloads directory by default.

## Most Common Patterns

```bash
# Custom output file
quickget.exe download -o ubuntu.iso https://releases.ubuntu.com/24.04/ubuntu-24.04.2-desktop-amd64.iso

# More workers for faster links
quickget.exe download -n 12 -o bigfile.iso https://example.com/bigfile.iso

# Resume: rerun the same command after interruption
quickget.exe download -n 12 -o bigfile.iso https://example.com/bigfile.iso
```

## Check Server Before Downloading

```bash
quickget.exe server-test https://example.com/bigfile.iso
```

Use output recommendations to choose `-n`.

## Monitor or Clean Resume State

```bash
# Inspect manifest state
quickget.exe status bigfile.iso

# Remove partial file + manifest if incomplete
quickget.exe clean bigfile.iso
```

## Verify Download Integrity

```bash
quickget.exe hash bigfile.iso
```

Compare the SHA-256 value with the checksum from the official source.

## Next Steps

- See [CLI Reference](cli-reference.md) for all commands and flags.
- See [Use Cases](use-cases.md) for production-like workflows.
