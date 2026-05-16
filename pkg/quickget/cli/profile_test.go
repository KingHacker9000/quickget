package cli

import "testing"

func TestParseProfileSizes(t *testing.T) {
	got, err := parseProfileSizes("10MB,100MB,1GB")
	if err != nil {
		t.Fatalf("parseProfileSizes error: %v", err)
	}
	if len(got) != 3 || got[0] != "10MB" || got[1] != "100MB" || got[2] != "1GB" {
		t.Fatalf("unexpected sizes: %#v", got)
	}
}

func TestParseProfileSizes_Dedup(t *testing.T) {
	got, err := parseProfileSizes("10mb,10MB,100MB")
	if err != nil {
		t.Fatalf("parseProfileSizes error: %v", err)
	}
	if len(got) != 2 || got[0] != "10MB" || got[1] != "100MB" {
		t.Fatalf("unexpected deduped sizes: %#v", got)
	}
}

func TestParseProfileSizes_Invalid(t *testing.T) {
	if _, err := parseProfileSizes("50MB"); err == nil {
		t.Fatalf("expected error for invalid size")
	}
}

func TestGenerateExhaustiveBaseConfigs_Count(t *testing.T) {
	buffers := []int{256 * 1024, 1024 * 1024}
	got := generateExhaustiveBaseConfigs("https://example.com/file.bin", []string{"10MB"}, buffers)
	// 1 size * 8 connections * 7 segments * 2 buffers * 2 http modes
	if len(got) != 224 {
		t.Fatalf("unexpected exhaustive matrix size: got=%d want=224", len(got))
	}
}

func TestFormatETA(t *testing.T) {
	if got := formatETA(3661 * 1000 * 1000 * 1000); got != "01:01:01" {
		t.Fatalf("unexpected ETA formatting: %s", got)
	}
}
