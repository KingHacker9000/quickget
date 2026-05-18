package nativehost

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestNativeMessageRoundTrip(t *testing.T) {
	in := map[string]any{"type": "ping"}
	var buf bytes.Buffer
	if err := WriteMessage(&buf, in); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	var out map[string]any
	if err := ReadMessage(&buf, &out); err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if out["type"] != "ping" {
		t.Fatalf("expected type=ping, got %+v", out)
	}
}

func TestHostRespondsToPing(t *testing.T) {
	var in bytes.Buffer
	if err := WriteMessage(&in, map[string]any{"type": "ping"}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var out bytes.Buffer
	host := NewHost(&in, &out, &bytes.Buffer{})
	if err := host.Serve(context.Background()); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	var resp map[string]any
	if err := ReadMessage(&out, &resp); err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp["type"] != "pong" {
		b, _ := json.Marshal(resp)
		t.Fatalf("expected pong response, got %s", string(b))
	}
}
