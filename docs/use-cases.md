# Use Cases

This page focuses on realistic command patterns you can reuse.

## 1) Large Public File With Throughput Focus

```bash
quickget.exe server-test https://proof.ovh.net/files/1Gb.dat
quickget.exe download -o 1Gb.dat -n 16 -d https://proof.ovh.net/files/1Gb.dat
```

Why this works:

- `server-test` confirms range behavior.
- Higher `-n` and dynamic splitting improve worker utilization on stable servers.

## 2) Authenticated Download With Custom Headers

```bash
quickget.exe download \
  -o private.pkg \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "X-Client: FastGet" \
  -user-agent "Mozilla/5.0 QuickGet" \
  https://example.com/private.pkg
```

Why this works:

- Header injection supports APIs or signed endpoints.
- Explicit user-agent helps with strict gateways.

## 3) Unreliable Server or Rate Limiting

```bash
quickget.exe server-test https://example.com/archive.zip
quickget.exe download -o archive.zip -n 2 -retries 8 https://example.com/archive.zip
```

Why this works:

- Low `-n` reduces pressure when the server rejects aggressive parallelism.
- Higher retries handle transient network resets.

## 4) Queue Mode for Predictable Segment Jobs

```bash
quickget.exe download -o image.iso -n 8 -queue-mode -segment-size 33554432 https://example.com/image.iso
```

Why this works:

- Queue mode uses fixed segment jobs, which can be easier to reason about when benchmarking.

## 5) Resume-Heavy Workflow

```bash
quickget.exe download -o huge.tar -n 12 https://example.com/huge.tar
# interrupt (Ctrl+C)
quickget.exe status huge.tar
quickget.exe download -o huge.tar -n 12 https://example.com/huge.tar
```

Why this works:

- Manifest preserves byte-range completion and retries only missing ranges.

## 6) Disk-Constrained or Variable Storage Targets

```bash
quickget.exe disk-test -o D:\temp\qg-disk-test.bin
quickget.exe download -o test.bin -auto-buffer https://proof.ovh.net/files/100Mb.dat
```

Why this works:

- Auto-buffer selects a tested buffer size for the actual target disk.
