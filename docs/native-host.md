# quickget-native-host

`quickget-native-host` is the Chrome Native Messaging bridge for QuickGet.

It sits between the Chrome extension and `quickget-agent`:

1. Extension sends native messages to `quickget-native-host`.
2. Host validates and forwards browser captures to local `quickget-agent` (`POST /captures`).
3. `quickget-agent` emits capture events for QuickGet Download Manager (QDM) to handle.

## Protocol

Native messaging frames use:

- 32-bit little-endian length prefix
- UTF-8 JSON payload
- stdin for requests, stdout for responses

Important:

- `stdout` is protocol-only. No logs on stdout.
- diagnostics are written to stderr only.

Supported request types:

- `ping` -> `pong`
- `status` -> running status snapshot
- `browser_capture` -> forwards capture to agent and returns `browser_capture_result`
- `open_qdm` -> attempts to ensure agent/QDM is running and returns `open_qdm_result`

## Windows install

Build:

```powershell
go build ./cmd/quickget-native-host
```

Install Chrome host registration:

```powershell
.\quickget-native-host.exe install-chrome
```

Optional explicit binary path:

```powershell
.\quickget-native-host.exe install-chrome -path "C:\Path\To\quickget-native-host.exe"
```

Uninstall:

```powershell
.\quickget-native-host.exe uninstall-chrome
```

Install writes a native host manifest named:

- `com.quickget.download_manager`

Manifest location:

- `%AppData%\QuickGet\native-host\com.quickget.download_manager.json`

Registry key (Chrome):

- `HKCU\Software\Google\Chrome\NativeMessagingHosts\com.quickget.download_manager`

If registry registration fails automatically, create that key manually and set its default value to the manifest path.

## Startup behavior

When handling `browser_capture` or `open_qdm`, if agent is not running:

- host checks `%AppData%\QuickGet\native-host.json` for optional paths:
  - `agent_executable_path`
  - `qdm_executable_path`
- it tries to launch configured agent first, then QDM.
- if still unavailable, host returns a clear error to the extension.

## Security and privacy

- agent auth token is loaded via existing `agent.LoadOrCreateToken` logic.
- capture forwarding is local-only to `http://127.0.0.1:19329`.
- cookies and Authorization headers are never logged.
- private headers are not printed in diagnostics.
