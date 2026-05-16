package bench

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRunBenchmark_DownloadsSampleAndCleansUp(t *testing.T) {
	data := []byte(strings.Repeat("a", 8192))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		rng := r.Header.Get("Range")
		if rng == "" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
			return
		}
		if rng != "bytes=0-1023" {
			t.Fatalf("unexpected range: %s", rng)
		}
		w.Header().Set("Content-Range", "bytes 0-1023/8192")
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[:1024])
	}))
	defer srv.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "bench.bin")
	res := RunBenchmark(context.Background(), BenchmarkConfig{
		TestSizeLabel:  "1KB",
		TargetBytes:    1024,
		SourceURL:      srv.URL,
		Connections:    1,
		QueueMode:      false,
		SegmentSize:    1024,
		BufferSize:     512,
		HTTPMode:       "http1",
		RepeatIndex:    1,
		OutputTempPath: outPath,
	})

	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.ErrorMessage)
	}
	if res.BytesDownloaded != 1024 {
		t.Fatalf("expected 1024 downloaded bytes, got %d", res.BytesDownloaded)
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("expected benchmark output cleanup, stat err=%v", err)
	}
}

func TestPruneBenchmarkConfigs_SegmentLargerThanFileRemoved(t *testing.T) {
	configs := []BenchmarkConfig{
		{TargetBytes: 10, Connections: 1, SegmentSize: 20, BufferSize: 4, HTTPMode: "auto"},
		{TargetBytes: 10, Connections: 1, SegmentSize: 10, BufferSize: 4, HTTPMode: "auto"},
	}
	got := PruneBenchmarkConfigs(configs)
	if len(got) != 1 {
		t.Fatalf("expected 1 config, got %d", len(got))
	}
	if got[0].SegmentSize != 10 {
		t.Fatalf("unexpected remaining config: %#v", got[0])
	}
}

func TestPruneBenchmarkConfigs_BufferLargerThanSegmentRemoved(t *testing.T) {
	configs := []BenchmarkConfig{
		{TargetBytes: 100, Connections: 1, SegmentSize: 32, BufferSize: 64, HTTPMode: "auto"},
		{TargetBytes: 100, Connections: 1, SegmentSize: 64, BufferSize: 32, HTTPMode: "auto"},
	}
	got := PruneBenchmarkConfigs(configs)
	if len(got) != 1 {
		t.Fatalf("expected 1 config, got %d", len(got))
	}
	if got[0].SegmentSize != 64 || got[0].BufferSize != 32 {
		t.Fatalf("unexpected remaining config: %#v", got[0])
	}
}

func TestPruneBenchmarkConfigs_ConnectionsLargerThanSegmentCountRemoved(t *testing.T) {
	configs := []BenchmarkConfig{
		{TargetBytes: 100, Connections: 8, SegmentSize: 50, BufferSize: 16, HTTPMode: "auto"},
		{TargetBytes: 100, Connections: 2, SegmentSize: 50, BufferSize: 16, HTTPMode: "auto"},
		{TargetBytes: 100, Connections: 1, SegmentSize: 100, BufferSize: 16, HTTPMode: "auto"},
	}
	got := PruneBenchmarkConfigs(configs)
	if len(got) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(got))
	}
}

func TestPruneBenchmarkConfigs_DuplicatesRemoved(t *testing.T) {
	configs := []BenchmarkConfig{
		{TargetBytes: 100, Connections: 2, SegmentSize: 50, BufferSize: 16, HTTPMode: "AUTO", RepeatIndex: 1, TestSizeLabel: "x"},
		{TargetBytes: 100, Connections: 2, SegmentSize: 50, BufferSize: 16, HTTPMode: "auto", RepeatIndex: 2, TestSizeLabel: "y"},
		{TargetBytes: 100, Connections: 2, SegmentSize: 50, BufferSize: 16, HTTPMode: "http1", RepeatIndex: 3, TestSizeLabel: "z"},
	}
	got := PruneBenchmarkConfigs(configs)
	if len(got) != 2 {
		t.Fatalf("expected 2 configs after dedup by effective identity, got %d", len(got))
	}
}

func TestPruneBenchmarkConfigs_ValidConfigsRemain(t *testing.T) {
	configs := []BenchmarkConfig{
		{TargetBytes: 10 * 1024 * 1024, Connections: 1, SegmentSize: 1 * 1024 * 1024, BufferSize: 256 * 1024, HTTPMode: "auto"},
		{TargetBytes: 10 * 1024 * 1024, Connections: 2, SegmentSize: 5 * 1024 * 1024, BufferSize: 512 * 1024, HTTPMode: "http1"},
	}
	got, stats := PruneBenchmarkConfigsWithStats(configs)
	if len(got) != 2 {
		t.Fatalf("expected valid configs to remain, got %d", len(got))
	}
	if stats.Generated != 2 || stats.Pruned != 0 || stats.Final != 2 {
		t.Fatalf("unexpected prune stats: %+v", stats)
	}
}
