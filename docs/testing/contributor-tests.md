# Contributor Tests

This page maps existing tests and defines high-value additions.

## Existing Test Coverage

- `pkg/quickget/cli/cli_test.go`
  - Argument normalization for legacy shorthand and URL placement.
  - Custom header parsing.
- `pkg/quickget/core/downloader_test.go`
  - Chunk splitting coverage and contiguity checks.
- `pkg/quickget/manifest/manifest_test.go`
  - Manifest range merge, missing range, normalize, totals behavior.
- `pkg/quickget/probe/probe_test.go`
  - Probe URL/filename/range behavior and helpers.

## Run Tests

```bash
go test ./...
```

## Recommended New Tests

## CLI

- Flag boundary tests for `-segment-size`, `-min-split-size`, `-min-dynamic-file-size`.
- `status` and `clean` command output behavior for missing manifest and complete manifest.

## Core

- Mode selection tests: single fallback when range unsupported/size unknown.
- Queue-mode segment planning for edge file sizes.
- Retry behavior assertions for transient segment failures.

## Manifest

- Resume compatibility checks across changed options.
- Corrupt JSON handling path for `status`/`clean` callers.

## Probe

- `server-test` recommendation mapping for `200`, `206`, `403`, `429`, `503`, `416`.

## Minimal Contributor Validation Before PR

1. `gofmt -w` on changed files.
2. `go test ./...` passes.
3. `mkdocs build --strict` passes if docs changed.
4. Any CLI text updates reflected in `docs/cli-reference.md` and `README.md` quick examples.
