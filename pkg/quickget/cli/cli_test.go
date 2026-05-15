package cli

import (
	"reflect"
	"testing"
)

func TestNormalizeDownloadArgs_URLAtEnd(t *testing.T) {
	got, err := normalizeDownloadArgs([]string{"-n", "8", "-o", "x.iso", "https://example.com/file.iso"})
	if err != nil {
		t.Fatalf("normalizeDownloadArgs error: %v", err)
	}
	want := []string{"-n", "8", "-o", "x.iso", "https://example.com/file.iso"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected normalized args: got %#v want %#v", got, want)
	}
}

func TestNormalizeDownloadArgs_URLAtStart(t *testing.T) {
	got, err := normalizeDownloadArgs([]string{"https://example.com/file.iso", "-n", "8", "-o", "x.iso"})
	if err != nil {
		t.Fatalf("normalizeDownloadArgs error: %v", err)
	}
	want := []string{"-n", "8", "-o", "x.iso", "https://example.com/file.iso"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected normalized args: got %#v want %#v", got, want)
	}
}

func TestParseCustomHeaders(t *testing.T) {
	h, err := parseCustomHeaders([]string{"Authorization: Bearer x", "X-Test: 1"})
	if err != nil {
		t.Fatalf("parseCustomHeaders error: %v", err)
	}
	if got := h.Get("Authorization"); got != "Bearer x" {
		t.Fatalf("unexpected Authorization header: %q", got)
	}
	if got := h.Get("X-Test"); got != "1" {
		t.Fatalf("unexpected X-Test header: %q", got)
	}
}
