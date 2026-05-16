package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestDownloadIntegration_NormalRangeServer(t *testing.T) {
	data := testPayloadBytes(256 * 1024)

	var headCount atomic.Int64
	var rangedCount atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			headCount.Add(1)
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if r.Header.Get("Range") != "" {
				rangedCount.Add(1)
			}
			serveRangedBytes(t, w, r, data, http.StatusPartialContent)
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer srv.Close()

	outputPath := runDownloadSuccess(t, context.Background(), srv.URL, nil, 4, 0, false, 0, t.TempDir())
	got := readFile(t, outputPath)

	if !bytes.Equal(got, data) {
		t.Fatalf("downloaded bytes mismatch: got=%d want=%d", len(got), len(data))
	}
	if headCount.Load() == 0 {
		t.Fatalf("expected at least one HEAD request")
	}
	if rangedCount.Load() == 0 {
		t.Fatalf("expected ranged GET requests")
	}
}

func TestDownloadIntegration_ServerIgnoresRange(t *testing.T) {
	data := testPayloadBytes(128 * 1024)
	var sawRange atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if r.Header.Get("Range") != "" {
				sawRange.Store(true)
			}
			// Intentionally ignore Range header.
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer srv.Close()

	_, err := runDownload(t, context.Background(), srv.URL, nil, 3, 0, false, 0, t.TempDir())
	assertErrorContains(t, err, "server ignored Range header")
	if !sawRange.Load() {
		t.Fatalf("expected server to receive Range header")
	}
}

func TestDownloadIntegration_RateLimitedRangeRequests(t *testing.T) {
	tests := []int{http.StatusTooManyRequests, http.StatusForbidden, http.StatusServiceUnavailable}
	for _, status := range tests {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			data := testPayloadBytes(96 * 1024)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodHead:
					w.Header().Set("Content-Length", strconv.Itoa(len(data)))
					w.Header().Set("Accept-Ranges", "bytes")
					w.WriteHeader(http.StatusOK)
				case http.MethodGet:
					if r.Header.Get("Range") == "" {
						t.Fatalf("expected Range header on GET")
					}
					w.WriteHeader(status)
				default:
					t.Fatalf("unexpected method: %s", r.Method)
				}
			}))
			defer srv.Close()

			_, err := runDownload(t, context.Background(), srv.URL, nil, 2, 0, false, 0, t.TempDir())
			assertErrorContains(t, err, "try lowering -n")
		})
	}
}

func TestDownloadIntegration_InvalidRange416(t *testing.T) {
	data := testPayloadBytes(64 * 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if r.Header.Get("Range") == "" {
				t.Fatalf("expected Range header")
			}
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer srv.Close()

	_, err := runDownload(t, context.Background(), srv.URL, nil, 2, 0, false, 0, t.TempDir())
	assertErrorContains(t, err, "server returned 416")
}

func TestDownloadIntegration_ResumeAfterCancellation(t *testing.T) {
	data := testPayloadBytes(512 * 1024)
	var requestNum atomic.Int64
	var interruptionMode atomic.Bool
	interruptionMode.Store(true)

	firstChunkDone := make(chan struct{})
	secondRangeStarted := make(chan struct{})
	var closeOnce sync.Once

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if r.Header.Get("Range") == "" {
				t.Fatalf("expected Range header")
			}

			n := requestNum.Add(1)
			if interruptionMode.Load() && n > 1 {
				closeOnce.Do(func() { close(secondRangeStarted) })
				<-r.Context().Done()
				return
			}

			serveRangedBytes(t, w, r, data, http.StatusPartialContent)
			if interruptionMode.Load() && n == 1 {
				close(firstChunkDone)
			}
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runDownload(t, ctx, srv.URL, nil, 2, 0, false, 0, dir)
		done <- err
	}()

	<-firstChunkDone
	<-secondRangeStarted
	cancel()

	firstErr := <-done
	assertErrorContains(t, firstErr, "download cancelled")

	interruptionMode.Store(false)

	outPath := filepath.Join(dir, "download.bin")
	manifestPath := outPath + ".quickget.json"
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("expected manifest to exist after interruption: %v", err)
	}

	resumePath := runDownloadSuccess(t, context.Background(), srv.URL, nil, 2, 0, false, 0, dir)
	got := readFile(t, resumePath)
	if !bytes.Equal(got, data) {
		t.Fatalf("resumed download bytes mismatch: got=%d want=%d", len(got), len(data))
	}
	if _, err := os.Stat(manifestPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected manifest removed after successful resume, err=%v", err)
	}
}

func TestDownloadIntegration_Redirect(t *testing.T) {
	data := testPayloadBytes(160 * 1024)
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			serveRangedBytes(t, w, r, data, http.StatusPartialContent)
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer final.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL+"/payload.bin", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	outputPath := runDownloadSuccess(t, context.Background(), redirect.URL+"/start", nil, 3, 0, false, 0, t.TempDir())
	got := readFile(t, outputPath)
	if !bytes.Equal(got, data) {
		t.Fatalf("redirect download bytes mismatch: got=%d want=%d", len(got), len(data))
	}
}

func TestDownloadIntegration_CustomHeadersRequired(t *testing.T) {
	data := testPayloadBytes(80 * 1024)
	const headerName = "X-QuickGet-Token"
	const headerValue = "secret-token"

	var sawHeaderOnHead atomic.Bool
	var sawHeaderOnGet atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(headerName) != headerValue {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		if r.Method == http.MethodHead {
			sawHeaderOnHead.Store(true)
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodGet {
			sawHeaderOnGet.Store(true)
			serveRangedBytes(t, w, r, data, http.StatusPartialContent)
			return
		}
		t.Fatalf("unexpected method: %s", r.Method)
	}))
	defer srv.Close()

	_, err := runDownload(t, context.Background(), srv.URL, nil, 2, 0, false, 0, t.TempDir())
	assertErrorContains(t, err, "403")

	headers := make(http.Header)
	headers.Set(headerName, headerValue)
	outPath := runDownloadSuccess(t, context.Background(), srv.URL, headers, 2, 0, false, 0, t.TempDir())
	got := readFile(t, outPath)
	if !bytes.Equal(got, data) {
		t.Fatalf("custom-header download bytes mismatch: got=%d want=%d", len(got), len(data))
	}
	if !sawHeaderOnHead.Load() || !sawHeaderOnGet.Load() {
		t.Fatalf("expected custom header on both HEAD and GET requests")
	}
}

func runDownloadSuccess(t *testing.T, ctx context.Context, rawURL string, headers http.Header, workers int, retries int, queueMode bool, segmentSize int64, outputDir string) string {
	t.Helper()
	outPath, err := runDownload(t, ctx, rawURL, headers, workers, retries, queueMode, segmentSize, outputDir)
	if err != nil {
		t.Fatalf("download returned error: %v", err)
	}
	return outPath
}

func runDownload(t *testing.T, ctx context.Context, rawURL string, headers http.Header, workers int, retries int, queueMode bool, segmentSize int64, outputDir string) (string, error) {
	t.Helper()

	req := DefaultRequest()
	req.URL = rawURL
	req.OutputDir = outputDir
	req.OutputPath = "download.bin"
	req.Stdout = io.Discard
	req.Workers = workers
	req.Retries = retries
	req.Dynamic = false
	req.QueueMode = queueMode
	if segmentSize > 0 {
		req.SegmentSize = segmentSize
	}
	if headers != nil {
		req.Headers = headers.Clone()
	}

	res, err := Download(ctx, req)
	if err != nil {
		return "", err
	}
	return res.OutputPath, nil
}

func serveRangedBytes(t *testing.T, w http.ResponseWriter, r *http.Request, data []byte, status int) {
	t.Helper()
	rangeValue := strings.TrimSpace(r.Header.Get("Range"))
	if rangeValue == "" {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}

	start, end, err := parseByteRange(rangeValue, int64(len(data)))
	if err != nil {
		t.Fatalf("invalid range header %q: %v", rangeValue, err)
	}

	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
	w.WriteHeader(status)
	_, _ = w.Write(data[start : end+1])
}

func parseByteRange(raw string, size int64) (int64, int64, error) {
	if !strings.HasPrefix(raw, "bytes=") {
		return 0, 0, fmt.Errorf("unsupported format: %q", raw)
	}
	spec := strings.TrimPrefix(raw, "bytes=")
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("malformed range: %q", raw)
	}
	start, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid start: %w", err)
	}
	endRaw := strings.TrimSpace(parts[1])
	var end int64
	if endRaw == "" {
		end = size - 1
	} else {
		end, err = strconv.ParseInt(endRaw, 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid end: %w", err)
		}
	}

	if start < 0 || end < start || start >= size || end >= size {
		return 0, 0, fmt.Errorf("range out of bounds: %d-%d for size %d", start, end, size)
	}
	return start, end, nil
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed reading %s: %v", path, err)
	}
	return b
}

func assertErrorContains(t *testing.T, err error, needle string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", needle)
	}
	if !strings.Contains(err.Error(), needle) {
		t.Fatalf("error mismatch: got %q want substring %q", err.Error(), needle)
	}
}

func testPayloadBytes(size int) []byte {
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = byte(i % 251)
	}
	return buf
}
