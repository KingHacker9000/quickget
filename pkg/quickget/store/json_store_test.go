package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quickget/pkg/quickget/api"
)

func TestLoadMissingFileReturnsEmptyState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-state.json")
	store := JSONStore{Path: path}

	state, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if state.Version != 0 {
		t.Fatalf("expected zero version, got %d", state.Version)
	}
	if len(state.Downloads) != 0 {
		t.Fatalf("expected no downloads, got %d", len(state.Downloads))
	}
	if !state.UpdatedAt.IsZero() {
		t.Fatalf("expected zero UpdatedAt, got %v", state.UpdatedAt)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state", "agent-state.json")
	store := JSONStore{Path: path}
	now := time.Now().UTC().Round(0)

	in := AgentState{
		Version: 1,
		Downloads: []api.DownloadSnapshot{{
			ID:         "d1",
			URL:        "https://example.com/file.bin",
			OutputPath: filepath.Join(dir, "file.bin"),
			Status:     "queued",
			CreatedAt:  now,
			UpdatedAt:  now,
		}},
		UpdatedAt: now,
	}

	if err := store.Save(in); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	out, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if out.Version != in.Version || !out.UpdatedAt.Equal(in.UpdatedAt) || len(out.Downloads) != 1 || out.Downloads[0].ID != "d1" {
		t.Fatalf("unexpected round trip state: %+v", out)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	if !strings.Contains(string(b), "\n  \"version\": 1,") {
		t.Fatalf("expected indented JSON, got: %s", string(b))
	}
	if !strings.HasSuffix(string(b), "\n") {
		t.Fatalf("expected trailing newline")
	}
}

func TestDefaultStatePathUsesUserConfigDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)

	path, err := DefaultStatePath()
	if err != nil {
		t.Fatalf("DefaultStatePath returned error: %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join("QuickGet", "agent-state.json")) {
		t.Fatalf("unexpected state path: %s", path)
	}
}

func TestLoadEmptyPathError(t *testing.T) {
	_, err := (JSONStore{}).Load()
	if err == nil {
		t.Fatalf("expected error for empty path")
	}
}

func TestSaveEmptyPathError(t *testing.T) {
	err := (JSONStore{}).Save(AgentState{})
	if err == nil {
		t.Fatalf("expected error for empty path")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-state.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	_, err := (JSONStore{Path: path}).Load()
	if err == nil {
		t.Fatalf("expected invalid JSON error")
	}
	var syntaxErr *os.PathError
	if errors.As(err, &syntaxErr) {
		t.Fatalf("expected JSON error, got path error: %v", err)
	}
}
