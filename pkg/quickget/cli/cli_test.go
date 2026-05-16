package cli

import (
	"bytes"
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

func TestNormalizeDownloadArgs_JSONEvents_URLAtStart(t *testing.T) {
	got, err := normalizeDownloadArgs([]string{"https://example.com/file.iso", "-json-events", "-n", "8"})
	if err != nil {
		t.Fatalf("normalizeDownloadArgs error: %v", err)
	}
	want := []string{"-json-events", "-n", "8", "https://example.com/file.iso"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected normalized args: got %#v want %#v", got, want)
	}
}

func TestParseDownloadOptions_JSONEvents(t *testing.T) {
	opts, err := parseDownloadOptions([]string{"-json-events", "-n", "4", "https://example.com/file.iso"}, bytes.NewBuffer(nil), "quickget")
	if err != nil {
		t.Fatalf("parseDownloadOptions error: %v", err)
	}
	if !opts.JsonEvents {
		t.Fatalf("expected JsonEvents=true")
	}
	if opts.Workers != 4 {
		t.Fatalf("expected workers=4, got %d", opts.Workers)
	}
}
