package manifest

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestMergeRanges(t *testing.T) {
	tests := []struct {
		name string
		in   []ByteRange
		want []ByteRange
	}{
		{
			name: "overlapping ranges",
			in:   []ByteRange{{Start: 0, End: 5}, {Start: 3, End: 10}},
			want: []ByteRange{{Start: 0, End: 10}},
		},
		{
			name: "adjacent ranges merge",
			in:   []ByteRange{{Start: 0, End: 4}, {Start: 5, End: 9}},
			want: []ByteRange{{Start: 0, End: 9}},
		},
		{
			name: "unordered ranges",
			in:   []ByteRange{{Start: 10, End: 12}, {Start: 0, End: 2}, {Start: 3, End: 5}},
			want: []ByteRange{{Start: 0, End: 5}, {Start: 10, End: 12}},
		},
		{
			name: "duplicate ranges",
			in:   []ByteRange{{Start: 7, End: 8}, {Start: 7, End: 8}, {Start: 7, End: 8}},
			want: []ByteRange{{Start: 7, End: 8}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MergeRanges(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unexpected merged ranges: got %#v want %#v", got, tc.want)
			}
		})
	}
}

func TestMissingRanges(t *testing.T) {
	tests := []struct {
		name      string
		start     int64
		end       int64
		completed []ByteRange
		want      []ByteRange
	}{
		{
			name:      "fully complete",
			start:     0,
			end:       9,
			completed: []ByteRange{{Start: 0, End: 9}},
			want:      []ByteRange{},
		},
		{
			name:      "partially complete",
			start:     0,
			end:       9,
			completed: []ByteRange{{Start: 0, End: 4}},
			want:      []ByteRange{{Start: 5, End: 9}},
		},
		{
			name:      "empty completion",
			start:     10,
			end:       14,
			completed: nil,
			want:      []ByteRange{{Start: 10, End: 14}},
		},
		{
			name:      "multiple gaps",
			start:     0,
			end:       9,
			completed: []ByteRange{{Start: 0, End: 1}, {Start: 4, End: 6}},
			want:      []ByteRange{{Start: 2, End: 3}, {Start: 7, End: 9}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MissingRanges(tc.start, tc.end, tc.completed)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unexpected missing ranges: got %#v want %#v", got, tc.want)
			}
		})
	}
}

func TestRangesCover(t *testing.T) {
	tests := []struct {
		name      string
		start     int64
		end       int64
		completed []ByteRange
		want      bool
	}{
		{
			name:      "full coverage",
			start:     0,
			end:       9,
			completed: []ByteRange{{Start: 0, End: 9}},
			want:      true,
		},
		{
			name:      "partial coverage",
			start:     0,
			end:       9,
			completed: []ByteRange{{Start: 0, End: 8}},
			want:      false,
		},
		{
			// Ranges that overlap the chunk boundaries should still count after clamping.
			name:      "edge overlaps",
			start:     10,
			end:       20,
			completed: []ByteRange{{Start: 0, End: 12}, {Start: 13, End: 30}},
			want:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RangesCover(tc.start, tc.end, tc.completed)
			if got != tc.want {
				t.Fatalf("unexpected coverage result: got %t want %t", got, tc.want)
			}
		})
	}
}

func TestManifestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.quickget.json")

	original := &DownloadManifest{
		URL:         "https://example.com/file.bin",
		OutputPath:  filepath.Join(dir, "file.bin"),
		TotalSize:   100,
		Connections: 4,
		QueueMode:   true,
		SegmentSize: 16,
		Chunks: []Chunk{
			{Index: 0, Start: 0, End: 49, DownloadedBytes: 50, Done: true, CompletedRanges: []ByteRange{{Start: 0, End: 49}}},
			{Index: 1, Start: 50, End: 99, DownloadedBytes: 10, Done: false, CompletedRanges: []ByteRange{{Start: 50, End: 59}}},
		},
	}

	if err := Save(path, original); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if !reflect.DeepEqual(reloaded, original) {
		t.Fatalf("manifest mismatch after save/load: got %#v want %#v", reloaded, original)
	}
}

func TestNormalizeChunk(t *testing.T) {
	c := Chunk{
		Start: 0,
		End:   9,
		CompletedRanges: []ByteRange{
			{Start: 0, End: 2},
			{Start: 3, End: 5},
			{Start: 8, End: 12},
		},
	}
	NormalizeChunk(&c)
	if c.DownloadedBytes != 8 {
		t.Fatalf("expected 8 downloaded bytes, got %d", c.DownloadedBytes)
	}
	if c.Done {
		t.Fatalf("expected incomplete chunk")
	}
}
