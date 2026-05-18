package nativehost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFormatCaptureForwardError(t *testing.T) {
	msg := formatCaptureForwardError(errors.New("api error (bad_request): capture_mode must be ask or auto"))
	if !strings.Contains(msg, "failed to forward capture to quickget-agent:") {
		t.Fatalf("expected standardized prefix, got %q", msg)
	}
	if !strings.Contains(msg, "capture_mode must be ask or auto") {
		t.Fatalf("expected underlying error detail, got %q", msg)
	}
}

func TestHandleBrowserCaptureIncludesUnderlyingAgentError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/captures":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"bad_request","message":"capture_mode must be ask or auto"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out := &bytes.Buffer{}
	host := &Host{
		in:       bytes.NewReader(nil),
		out:      out,
		errLog:   log.New(io.Discard, "", 0),
		agentURL: srv.URL,
	}

	origTokenLoader := loadOrCreateToken
	loadOrCreateToken = func() (string, error) {
		return "unit-test-token", nil
	}
	defer func() { loadOrCreateToken = origTokenLoader }()

	payload, err := json.Marshal(map[string]any{
		"type": "browser_capture",
		"payload": map[string]any{
			"url":                "https://proof.ovh.net/files/100Mb.dat",
			"chrome_download_id": -1,
			"capture_mode":       "auto",
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	if err := host.handleBrowserCapture(context.Background(), payload); err != nil {
		t.Fatalf("handleBrowserCapture returned error: %v", err)
	}

	var resp map[string]any
	if err := ReadMessage(out, &resp); err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if ok, _ := resp["ok"].(bool); ok {
		t.Fatalf("expected ok=false, got %+v", resp)
	}
	message, _ := resp["message"].(string)
	if !strings.Contains(message, "failed to forward capture to quickget-agent:") {
		t.Fatalf("expected standardized prefix, got %q", message)
	}
	if !strings.Contains(message, "capture_mode must be ask or auto") {
		t.Fatalf("expected underlying detail, got %q", message)
	}
}
