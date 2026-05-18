package agent

import (
	"testing"

	"quickget/pkg/quickget/api"
	"quickget/pkg/quickget/events"
)

func TestManagerCaptureLifecycle(t *testing.T) {
	dl := newFakeDownloader()
	dl.block = true
	st := &fakeStore{}
	m := newManagerWithFake(t, dl, st)
	sub, unsub := m.Events().Subscribe()
	defer unsub()

	capReq := api.BrowserCaptureRequest{
		Source:      "chrome-auto-capture",
		Browser:     "chrome",
		URL:         "https://unit.test/file.bin",
		CaptureMode: "ask",
	}
	capture, err := m.CreateCapture(capReq)
	if err != nil {
		t.Fatalf("CreateCapture error: %v", err)
	}
	if capture.Status != CaptureStatusPending {
		t.Fatalf("expected pending, got %s", capture.Status)
	}
	_ = waitEvent(t, sub, events.EventCaptureRequested)

	startReq := api.StartCaptureDownloadRequest{
		OutputPath:      "file.bin",
		Directory:       ".",
		DuplicateAction: "overwrite",
	}
	updated, snap, err := m.StartCaptureDownload(capture.ID, startReq)
	if err != nil {
		t.Fatalf("StartCaptureDownload error: %v", err)
	}
	if updated.Status != CaptureStatusStarted {
		t.Fatalf("expected started, got %s", updated.Status)
	}
	if snap.ID == "" {
		t.Fatal("expected download snapshot id")
	}
	dl.waitStarted(t)
	_ = waitEvent(t, sub, events.EventCaptureStarted)
}

func TestRejectCapture(t *testing.T) {
	m := NewManager(&fakeStore{})
	capture, err := m.CreateCapture(api.BrowserCaptureRequest{
		Source:      "chrome-auto-capture",
		Browser:     "chrome",
		URL:         "https://unit.test/file.bin",
		CaptureMode: "ask",
	})
	if err != nil {
		t.Fatalf("CreateCapture error: %v", err)
	}
	out, err := m.RejectCapture(capture.ID)
	if err != nil {
		t.Fatalf("RejectCapture error: %v", err)
	}
	if out.Status != CaptureStatusRejected {
		t.Fatalf("expected rejected, got %s", out.Status)
	}
}
