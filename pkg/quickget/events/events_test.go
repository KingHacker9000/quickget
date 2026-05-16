package events

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEmitterEmit_WritesNDJSONLine(t *testing.T) {
	var buf bytes.Buffer
	emitter := NewEmitter(&buf)

	if err := emitter.Emit(map[string]any{"type": "start", "connections": 8}); err != nil {
		t.Fatalf("Emit error: %v", err)
	}

	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("expected newline suffix, got %q", out)
	}

	line := strings.TrimSpace(out)
	var obj map[string]any
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		t.Fatalf("line is not valid JSON: %v", err)
	}
	if obj["type"] != "start" {
		t.Fatalf("expected type=start, got %v", obj["type"])
	}
}
