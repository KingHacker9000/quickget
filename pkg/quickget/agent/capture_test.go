package agent

import (
	"net/http"
	"net/http/httptest"
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

func TestCreateCaptureAutoStartsDownload(t *testing.T) {
	dl := newFakeDownloader()
	dl.block = true
	m := newManagerWithFake(t, dl, &fakeStore{})
	sub, unsub := m.Events().Subscribe()
	defer unsub()

	capture, err := m.CreateCapture(api.BrowserCaptureRequest{
		Source:            "chrome-auto-capture",
		Browser:           "chrome",
		URL:               "https://unit.test/auto.bin",
		SuggestedFilename: "auto.bin",
		CaptureMode:       "auto",
	})
	if err != nil {
		t.Fatalf("CreateCapture error: %v", err)
	}
	if capture.Status != CaptureStatusStarted {
		t.Fatalf("expected started, got %s", capture.Status)
	}

	dl.waitStarted(t)
	_ = waitEvent(t, sub, events.EventCaptureRequested)
	_ = waitEvent(t, sub, events.EventCaptureStarted)
}

func TestCreateCaptureEnrichesMetadataFromProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="report.csv"`)
		w.Header().Set("Content-Length", "12345")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	m := NewManager(&fakeStore{})
	capture, err := m.CreateCapture(api.BrowserCaptureRequest{
		Source:      "chrome-auto-capture",
		Browser:     "chrome",
		URL:         srv.URL + "/download?id=42",
		CaptureMode: "ask",
	})
	if err != nil {
		t.Fatalf("CreateCapture error: %v", err)
	}
	if capture.Request.SuggestedFilename != "report.csv" {
		t.Fatalf("expected suggested filename from probe, got %q", capture.Request.SuggestedFilename)
	}
	if capture.Request.TotalBytes != 12345 {
		t.Fatalf("expected total bytes from probe, got %d", capture.Request.TotalBytes)
	}
	if capture.Request.FinalURL == "" {
		t.Fatal("expected final URL to be populated from probe")
	}
}
