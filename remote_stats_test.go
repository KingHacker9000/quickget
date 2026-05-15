package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchRemoteFileStats_ContentLengthValid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Fatalf("expected HEAD request, got %s", r.Method)
		}
		w.Header().Set("Content-Length", "12345")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stats, err := FetchRemoteFileStats(srv.Client(), srv.URL, nil, DefaultUserAgent)
	if err != nil {
		t.Fatalf("FetchRemoteFileStats returned error: %v", err)
	}
	if stats.Size != 12345 {
		t.Fatalf("expected size 12345, got %d", stats.Size)
	}
}

func TestFetchRemoteFileStats_ContentLengthMissingIsUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stats, err := FetchRemoteFileStats(srv.Client(), srv.URL, nil, DefaultUserAgent)
	if err != nil {
		t.Fatalf("FetchRemoteFileStats returned error: %v", err)
	}
	if stats.Size != -1 {
		t.Fatalf("expected size -1 for missing content length, got %d", stats.Size)
	}
}

func TestParseContentLengthHeader_InvalidIsUnknown(t *testing.T) {
	size := parseContentLengthHeader("not-a-number")
	if size != -1 {
		t.Fatalf("expected size -1 for invalid content length, got %d", size)
	}
}

func TestFetchRemoteFileStats_AcceptRangesBytesIsSupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stats, err := FetchRemoteFileStats(srv.Client(), srv.URL, nil, DefaultUserAgent)
	if err != nil {
		t.Fatalf("FetchRemoteFileStats returned error: %v", err)
	}
	if !stats.RangeSupported {
		t.Fatalf("expected range support for Accept-Ranges=bytes")
	}
}

func TestFetchRemoteFileStats_AcceptRangesOtherOrMissingIsUnsupported(t *testing.T) {
	tests := []struct {
		name         string
		acceptRanges string
	}{
		{name: "other", acceptRanges: "none"},
		{name: "missing", acceptRanges: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.acceptRanges != "" {
					w.Header().Set("Accept-Ranges", tc.acceptRanges)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			stats, err := FetchRemoteFileStats(srv.Client(), srv.URL, nil, DefaultUserAgent)
			if err != nil {
				t.Fatalf("FetchRemoteFileStats returned error: %v", err)
			}
			if stats.RangeSupported {
				t.Fatalf("expected range unsupported for Accept-Ranges=%q", tc.acceptRanges)
			}
		})
	}
}
