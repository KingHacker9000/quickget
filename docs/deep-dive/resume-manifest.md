# Deep Dive: Resume Manifest

QuickGet stores progress in:

```text
<output-file>.quickget.json
```

## Manifest Contents

The manifest tracks:

- Input URL and resolved output path.
- Total size.
- Connection count and queue settings.
- Chunk boundaries.
- Completed byte ranges per chunk.

## Lifecycle

1. Start download.
2. Create or load manifest.
3. Persist progress periodically and on task completion.
4. On successful completion, remove manifest.

## Resume Rules

Resume continues only when critical settings match:

- URL
- Output path
- Total size
- Connections
- Queue-mode compatibility (including segment size)

If they do not match, QuickGet starts a fresh manifest.

## `status` Command Semantics

```bash
quickget.exe status file.iso
```

Outputs:

- Manifest path
- URL/output metadata
- Total/chunk completion counts
- Byte progress and percentage
- State (`incomplete` or `complete`)
- Raw manifest JSON

## `clean` Command Semantics

```bash
quickget.exe clean file.iso
```

Behavior:

- If manifest is missing: reports and leaves output unchanged.
- If manifest exists and download incomplete: removes manifest and partial output.
- If manifest exists and download complete: removes manifest and keeps output.

## Failure Recovery

- If process is interrupted, rerun the same `download` command.
- If manifest is corrupt or stale, use `clean` and restart.
- If server-side file changed mid-download, restart clean for integrity.
