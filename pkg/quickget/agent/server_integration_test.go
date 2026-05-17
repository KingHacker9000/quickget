package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"quickget/pkg/quickget/api"
)

const testBearerToken = "test-token"

func newTestAgentServer(t *testing.T, dl *fakeDownloader) *httptest.Server {
	t.Helper()
	m := NewManager(&fakeStore{})
	m.SetDownloader(dl)
	m.SetProfilerRunner(fakeProfilerRunner{})
	s := NewServer(m, testBearerToken, "test")
	return httptest.NewServer(s)
}

type fakeProfilerRunner struct{}

func (fakeProfilerRunner) Run(ctx context.Context, req ProfilerRunRequest, runID string, emit func(stage, msg string, data map[string]any)) (profilerRunResult, error) {
	emit("prepare", "prep", map[string]any{"run_id": runID, "step_index": 1, "step_total": 2})
	select {
	case <-ctx.Done():
		return profilerRunResult{}, ctx.Err()
	case <-time.After(120 * time.Millisecond):
	}
	emit("benchmark", "bench", map[string]any{"run_id": runID, "step_index": 2, "step_total": 2})
	return profilerRunResult{
		Recommendation: ProfilerRecommendation{Connections: 4, QueueMode: true, SegmentSize: 16777216, BufferSize: 1048576, HTTP1: false},
		Artifacts:      ProfilerArtifacts{ProfileDir: ".quickget/profiles/test", RawCSV: ".quickget/profiles/test/raw_results.csv", SummaryCSV: ".quickget/profiles/test/summary.csv"},
	}, nil
}

func authReq(t *testing.T, method, url string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testBearerToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func decodeBody[T any](t *testing.T, r io.Reader) T {
	t.Helper()
	var out T
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return out
}

func createJob(t *testing.T, c *http.Client, baseURL string) api.DownloadSnapshot {
	t.Helper()
	payload := []byte(`{"url":"https://unit.test/file.bin","outputPath":"file.bin","directory":"."}`)
	resp, err := c.Do(authReq(t, http.MethodPost, baseURL+"/downloads", bytes.NewReader(payload)))
	if err != nil {
		t.Fatalf("POST /downloads: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /downloads status=%d", resp.StatusCode)
	}
	return decodeBody[api.DownloadSnapshot](t, resp.Body)
}

func TestServerIntegrationHTTPAPI(t *testing.T) {
	dl := newFakeDownloader()
	dl.block = true
	srv := newTestAgentServer(t, dl)
	defer srv.Close()

	client := srv.Client()

	t.Run("GET /health returns ok without auth", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/health")
		if err != nil {
			t.Fatalf("GET /health: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", resp.StatusCode)
		}
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode /health: %v", err)
		}
		if ok, _ := body["ok"].(bool); !ok {
			t.Fatalf("expected ok=true, got %+v", body)
		}
	})

	t.Run("GET /downloads without auth returns 401", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/downloads")
		if err != nil {
			t.Fatalf("GET /downloads: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status=%d", resp.StatusCode)
		}
	})

	var created api.DownloadSnapshot

	t.Run("GET /downloads with valid bearer token returns list", func(t *testing.T) {
		created = createJob(t, client, srv.URL)
		dl.waitStarted(t)

		resp, err := client.Do(authReq(t, http.MethodGet, srv.URL+"/downloads", nil))
		if err != nil {
			t.Fatalf("GET /downloads auth: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", resp.StatusCode)
		}
		list := decodeBody[[]api.DownloadSnapshot](t, resp.Body)
		if len(list) == 0 {
			t.Fatal("expected non-empty downloads list")
		}
	})

	t.Run("POST /downloads with trailing JSON tokens returns 400", func(t *testing.T) {
		payload := []byte(`{"url":"https://unit.test/file.bin"}{"extra":true}`)
		resp, err := client.Do(authReq(t, http.MethodPost, srv.URL+"/downloads", bytes.NewReader(payload)))
		if err != nil {
			t.Fatalf("POST /downloads malformed JSON: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status=%d", resp.StatusCode)
		}
	})

	t.Run("GET /downloads/{id} returns that job", func(t *testing.T) {
		resp, err := client.Do(authReq(t, http.MethodGet, srv.URL+"/downloads/"+created.ID, nil))
		if err != nil {
			t.Fatalf("GET /downloads/{id}: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", resp.StatusCode)
		}
		snap := decodeBody[api.DownloadSnapshot](t, resp.Body)
		if snap.ID != created.ID {
			t.Fatalf("expected id=%s got %s", created.ID, snap.ID)
		}
	})

	t.Run("POST /downloads/{id}/pause pauses job", func(t *testing.T) {
		resp, err := client.Do(authReq(t, http.MethodPost, srv.URL+"/downloads/"+created.ID+"/pause", nil))
		if err != nil {
			t.Fatalf("pause: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", resp.StatusCode)
		}
		snap := decodeBody[api.DownloadSnapshot](t, resp.Body)
		if snap.Status != JobStatusPaused {
			t.Fatalf("expected paused got %s", snap.Status)
		}
	})

	t.Run("POST /downloads/{id}/resume resumes job", func(t *testing.T) {
		resp, err := client.Do(authReq(t, http.MethodPost, srv.URL+"/downloads/"+created.ID+"/resume", nil))
		if err != nil {
			t.Fatalf("resume: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", resp.StatusCode)
		}
		snap := decodeBody[api.DownloadSnapshot](t, resp.Body)
		if snap.Status != JobStatusRunning {
			t.Fatalf("expected running got %s", snap.Status)
		}
	})

	t.Run("POST /downloads/{id}/cancel cancels job", func(t *testing.T) {
		resp, err := client.Do(authReq(t, http.MethodPost, srv.URL+"/downloads/"+created.ID+"/cancel", nil))
		if err != nil {
			t.Fatalf("cancel: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", resp.StatusCode)
		}
		snap := decodeBody[api.DownloadSnapshot](t, resp.Body)
		if snap.Status != JobStatusCancelled {
			t.Fatalf("expected cancelled got %s", snap.Status)
		}
	})

	t.Run("POST /downloads/{id}/delete removes job", func(t *testing.T) {
		resp, err := client.Do(authReq(t, http.MethodPost, srv.URL+"/downloads/"+created.ID+"/delete", nil))
		if err != nil {
			t.Fatalf("delete: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", resp.StatusCode)
		}

		check, err := client.Do(authReq(t, http.MethodGet, srv.URL+"/downloads/"+created.ID, nil))
		if err != nil {
			t.Fatalf("verify delete: %v", err)
		}
		defer check.Body.Close()
		if check.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 after delete, got %d", check.StatusCode)
		}
	})

	t.Run("GET /events returns SSE stream and emits JSON events", func(t *testing.T) {
		sseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(sseCtx, http.MethodGet, srv.URL+"/events", nil)
		if err != nil {
			t.Fatalf("new events request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+testBearerToken)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET /events: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
			t.Fatalf("unexpected content-type: %q", ct)
		}

		eventsDone := make(chan []byte, 1)
		errCh := make(chan error, 1)
		go func() {
			r := bufio.NewReader(resp.Body)
			for {
				line, err := r.ReadString('\n')
				if err != nil {
					errCh <- err
					return
				}
				if strings.HasPrefix(line, "data: ") {
					eventsDone <- []byte(strings.TrimSpace(strings.TrimPrefix(line, "data: ")))
					return
				}
			}
		}()

		_ = createJob(t, client, srv.URL)

		select {
		case data := <-eventsDone:
			var ev map[string]any
			if err := json.Unmarshal(data, &ev); err != nil {
				t.Fatalf("invalid event JSON: %v", err)
			}
			if _, ok := ev["type"]; !ok {
				t.Fatalf("expected event type field in payload: %s", string(data))
			}
		case err := <-errCh:
			t.Fatalf("stream read error: %v", err)
		case <-sseCtx.Done():
			t.Fatal("timed out waiting for SSE event")
		}
	})

	t.Run("GET /profiler returns state", func(t *testing.T) {
		resp, err := client.Do(authReq(t, http.MethodGet, srv.URL+"/profiler", nil))
		if err != nil {
			t.Fatalf("GET /profiler: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", resp.StatusCode)
		}
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode /profiler: %v", err)
		}
		if _, ok := body["status"]; !ok {
			t.Fatalf("expected status field in /profiler response")
		}
	})

	t.Run("POST /profiler/run starts profiler and conflicts while running", func(t *testing.T) {
		resp, err := client.Do(authReq(t, http.MethodPost, srv.URL+"/profiler/run", bytes.NewReader([]byte(`{}`))))
		if err != nil {
			t.Fatalf("POST /profiler/run: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("status=%d", resp.StatusCode)
		}

		respConflict, err := client.Do(authReq(t, http.MethodPost, srv.URL+"/profiler/run", bytes.NewReader([]byte(`{}`))))
		if err != nil {
			t.Fatalf("POST /profiler/run conflict: %v", err)
		}
		defer respConflict.Body.Close()
		if respConflict.StatusCode != http.StatusConflict {
			t.Fatalf("expected conflict, got status=%d", respConflict.StatusCode)
		}

		respCancel, err := client.Do(authReq(t, http.MethodPost, srv.URL+"/profiler/cancel", bytes.NewReader([]byte(`{}`))))
		if err != nil {
			t.Fatalf("POST /profiler/cancel: %v", err)
		}
		defer respCancel.Body.Close()
		if respCancel.StatusCode != http.StatusAccepted {
			t.Fatalf("expected accepted from cancel, got status=%d", respCancel.StatusCode)
		}

		time.Sleep(150 * time.Millisecond)
		resp2, err := client.Do(authReq(t, http.MethodGet, srv.URL+"/profiler", nil))
		if err != nil {
			t.Fatalf("GET /profiler after run: %v", err)
		}
		defer resp2.Body.Close()
		if resp2.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", resp2.StatusCode)
		}
	})

	dl.allowReturn()
}
