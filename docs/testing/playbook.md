# Testing Playbook (User-Facing)

These scenarios help validate behavior from the CLI user perspective.

## Setup

- Build binary once:

```bash
go build -o quickget.exe ./cmd/quickget
```

- Create a temporary download directory for tests.

## Scenario 1: Baseline Download

```bash
quickget.exe download -dir .\downloads https://proof.ovh.net/files/100Mb.dat
```

Expected:

- File completes successfully.
- No manifest file remains after success.

## Scenario 2: Resume After Interrupt

1. Start large download.
2. Interrupt with Ctrl+C.
3. Run status.
4. Resume with same command.

```bash
quickget.exe download -o 1Gb.dat -dir .\downloads https://proof.ovh.net/files/1Gb.dat
quickget.exe status .\downloads\1Gb.dat
quickget.exe download -o 1Gb.dat -dir .\downloads https://proof.ovh.net/files/1Gb.dat
```

Expected:

- Status shows partial progress after interruption.
- Resume finishes and manifest is removed.

## Scenario 3: Server Capability Probe

```bash
quickget.exe inspect https://proof.ovh.net/files/100Mb.dat
quickget.exe filestats https://proof.ovh.net/files/100Mb.dat
quickget.exe server-test https://proof.ovh.net/files/100Mb.dat
```

Expected:

- Outputs include resolved URL, size, and range details.
- `server-test` prints a recommendation for `-n`.

## Scenario 4: Queue Mode

```bash
quickget.exe download -o queue.dat -n 8 -queue-mode -segment-size 16777216 https://proof.ovh.net/files/100Mb.dat
```

Expected:

- Download succeeds in queue mode.
- Throughput may differ from dynamic mode.

## Scenario 5: Integrity Check

```bash
quickget.exe hash .\downloads\100Mb.dat
```

Expected:

- SHA-256 hash is printed in `sum  file` format.

## Scenario 6: Cleanup Behavior

```bash
quickget.exe clean .\downloads\1Gb.dat
```

Expected:

- Removes partial output only when incomplete.
- Preserves completed files.

## Scenario 7: Invalid Input Handling

```bash
quickget.exe download
quickget.exe download -n 0 https://example.com/file.bin
quickget.exe download -H "InvalidHeader" https://example.com/file.bin
```

Expected:

- Clear validation errors for missing URL, invalid flags, or malformed headers.
