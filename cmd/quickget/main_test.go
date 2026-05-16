package main

import "testing"

func TestHasJSONEventsFlag(t *testing.T) {
	if !hasJSONEventsFlag([]string{"download", "-json-events", "https://example.com"}) {
		t.Fatalf("expected true when -json-events is present")
	}
	if hasJSONEventsFlag([]string{"download", "-n", "4", "https://example.com"}) {
		t.Fatalf("expected false when -json-events is not present")
	}
}
