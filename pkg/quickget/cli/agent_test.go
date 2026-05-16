package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quickget/pkg/quickget/api"
)

func setUserConfigEnv(t *testing.T, root string) {
	t.Helper()
	t.Setenv("APPDATA", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
}

func writeAgentTokenFile(t *testing.T, token string) string {
	t.Helper()
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("user config dir: %v", err)
	}
	path := filepath.Join(cfgDir, "QuickGet", "agent-token")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir token dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	return path
}

func TestRunAgentHealthDoesNotRequireTokenFile(t *testing.T) {
	root := t.TempDir()
	setUserConfigEnv(t, root)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"name":    "quickget-agent",
			"version": "test",
		})
	}))
	defer srv.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runAgentHealth([]string{"-agent", srv.URL}, &stdout, &stderr, "quickget"); err != nil {
		t.Fatalf("runAgentHealth error: %v", err)
	}
	if !strings.Contains(stdout.String(), "Healthy: true") {
		t.Fatalf("unexpected health output: %q", stdout.String())
	}
}

func TestRunAgentDownloadAcceptsAgentFlagAndInterspersedURL(t *testing.T) {
	root := t.TempDir()
	setUserConfigEnv(t, root)
	writeAgentTokenFile(t, "tok")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/downloads" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Fatalf("unexpected authorization header: %q", got)
		}

		var req api.CreateDownloadRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.URL != "https://example.com/file.bin" {
			t.Fatalf("unexpected URL: %q", req.URL)
		}
		if req.Connections != 4 {
			t.Fatalf("unexpected connections: %d", req.Connections)
		}
		if req.Retries != 2 {
			t.Fatalf("unexpected retries: %d", req.Retries)
		}
		if !req.QueueMode {
			t.Fatalf("expected queue mode true")
		}
		if req.SegmentSize != 2048 {
			t.Fatalf("unexpected segment size: %d", req.SegmentSize)
		}
		if req.BufferSize != 4096 {
			t.Fatalf("unexpected buffer size: %d", req.BufferSize)
		}
		if !req.AutoBuffer {
			t.Fatalf("expected auto buffer true")
		}
		if req.HTTP1 {
			t.Fatalf("expected http1=false")
		}
		if req.UserAgent != "QuickGet-Test-UA" {
			t.Fatalf("unexpected user agent: %q", req.UserAgent)
		}
		if req.Headers["X-Test"] != "1" {
			t.Fatalf("unexpected X-Test header: %#v", req.Headers)
		}
		if req.OutputPath != "file.bin" {
			t.Fatalf("expected derived output file name, got %q", req.OutputPath)
		}

		_ = json.NewEncoder(w).Encode(api.DownloadSnapshot{
			ID:         "job-1",
			Status:     "queued",
			OutputPath: "file.bin",
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		})
	}))
	defer srv.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	args := []string{
		"https://example.com/file.bin",
		"-agent", srv.URL,
		"-n", "4",
		"-retries", "2",
		"-queue-mode",
		"-segment-size", "2048",
		"-buffer-size", "4096",
		"-auto-buffer",
		"-http1=false",
		"-user-agent", "QuickGet-Test-UA",
		"-H", "X-Test: 1",
	}
	if err := runAgentDownload(args, &stdout, &stderr, "quickget"); err != nil {
		t.Fatalf("runAgentDownload error: %v", err)
	}
	if !strings.Contains(stdout.String(), "Added: job-1") {
		t.Fatalf("unexpected download output: %q", stdout.String())
	}
}

func TestRunAgentPauseAcceptsAgentFlag(t *testing.T) {
	root := t.TempDir()
	setUserConfigEnv(t, root)
	writeAgentTokenFile(t, "tok")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/downloads/job-1/pause" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(api.DownloadSnapshot{
			ID:        "job-1",
			Status:    "paused",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		})
	}))
	defer srv.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runAgentPause([]string{"job-1", "-agent", srv.URL}, &stdout, &stderr, "quickget"); err != nil {
		t.Fatalf("runAgentPause error: %v", err)
	}
	if !strings.Contains(stdout.String(), "Status: paused") {
		t.Fatalf("unexpected pause output: %q", stdout.String())
	}
}

func TestRunAgentDeleteAcceptsAgentFlag(t *testing.T) {
	root := t.TempDir()
	setUserConfigEnv(t, root)
	writeAgentTokenFile(t, "tok")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/downloads/job-1/delete" {
			http.NotFound(w, r)
			return
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode delete body: %v", err)
		}
		if v, ok := body["delete_files"].(bool); !ok || !v {
			t.Fatalf("expected delete_files=true, got %#v", body)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": "job-1", "delete_files": true})
	}))
	defer srv.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runAgentDelete([]string{"-agent", srv.URL, "job-1", "-delete-files"}, &stdout, &stderr, "quickget"); err != nil {
		t.Fatalf("runAgentDelete error: %v", err)
	}
	if !strings.Contains(stdout.String(), "Deleted: job-1 (delete-files=true)") {
		t.Fatalf("unexpected delete output: %q", stdout.String())
	}
}
