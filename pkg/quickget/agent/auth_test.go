package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateTokenCreatesThenLoads(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("APPDATA", configRoot)
	t.Setenv("XDG_CONFIG_HOME", configRoot)

	t1, err := LoadOrCreateToken()
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if t1 == "" {
		t.Fatal("token is empty")
	}

	t2, err := LoadOrCreateToken()
	if err != nil {
		t.Fatalf("load token: %v", err)
	}
	if t1 != t2 {
		t.Fatalf("expected persisted token, got %q != %q", t1, t2)
	}

	cfgDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("user config dir: %v", err)
	}
	path := filepath.Join(cfgDir, "QuickGet", tokenFileName)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("token file missing: %v", err)
	}
}

func TestCheckBearerToken(t *testing.T) {
	expected := "abc123"
	if !CheckBearerToken("Bearer abc123", expected) {
		t.Fatal("expected valid token")
	}
	if !CheckBearerToken("bearer abc123", expected) {
		t.Fatal("expected case-insensitive bearer scheme")
	}
	if CheckBearerToken("Bearer wrong", expected) {
		t.Fatal("unexpected valid token")
	}
	if CheckBearerToken("Basic abc123", expected) {
		t.Fatal("unexpected valid auth scheme")
	}
	if CheckBearerToken("", expected) {
		t.Fatal("unexpected empty header accepted")
	}
}
