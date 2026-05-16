# FAQ and Troubleshooting

## Quick Diagnostics Flow

1. Run `server-test` on the URL.
2. Start with conservative `-n` if the server warns.
3. Check `status` if interrupted.
4. Use `clean` only when reset is required.

## Why Did QuickGet Use Single Mode?

Single mode is selected when:

- Server does not support range requests.
- File size is unknown.
- You set `-n 1`.

Use `server-test` and `inspect` to confirm server behavior.

## I See `server ignored Range header`

The server replied `200 OK` to a ranged request. Use:

```bash
quickget.exe download -n 1 -o file.bin <url>
```

## I See `429`, `403`, or `503`

Server is likely rate limiting or rejecting aggressive concurrency.

Try:

```bash
quickget.exe download -n 2 -retries 8 -o file.bin <url>
```

## Resume Is Not Continuing

Checklist:

- Ensure same URL and output file.
- Ensure same mode-critical settings (connections, queue mode, segment size).
- Check that `<output>.quickget.json` exists.
- If stale/corrupt, run `clean` and restart.

## Download Fails With Disk Space Errors

QuickGet checks available free space before transfer for known-size files.

Actions:

- Free disk space.
- Change destination directory with `-dir`.
- Use a larger target volume.

## Header Issues With `-H`

`-H` must be exactly `Header-Name: value`.

Example:

```bash
quickget.exe download -H "Authorization: Bearer TOKEN" <url>
```

## How Do I Benchmark Settings Safely?

- Use a legal public test file.
- Prefer `profile` for structured benchmark runs:

```bash
quickget.exe profile --level normal --sizes 10MB,100MB,1GB --repeats 3
```

- Use `--url` to pin a trusted endpoint in your region.
- Keep one variable changed per manual run.
- Record throughput and error/retry behavior.

## Where to Report Bugs

Open a GitHub issue with:

- Command used.
- URL type (public/private, no secrets).
- OS and Go version.
- Error output.
- Whether resume manifest existed.
