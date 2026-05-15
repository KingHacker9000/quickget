package core

import "testing"

func TestSplitIntoChunks_CoversExactlyWithoutGapsOrOverlaps(t *testing.T) {
	tests := []struct {
		name        string
		size        int64
		connections int
	}{
		{name: "even split", size: 100, connections: 4},
		{name: "uneven split", size: 10, connections: 3},
		{name: "tiny file many workers", size: 3, connections: 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chunks := splitIntoChunks(tc.size, tc.connections)
			expectedChunks := tc.connections
			if int64(expectedChunks) > tc.size {
				expectedChunks = int(tc.size)
			}
			if len(chunks) != expectedChunks {
				t.Fatalf("expected %d chunks, got %d", expectedChunks, len(chunks))
			}

			if chunks[0].Start != 0 {
				t.Fatalf("first chunk must start at 0, got %d", chunks[0].Start)
			}
			if chunks[len(chunks)-1].End != tc.size-1 {
				t.Fatalf("last chunk must end at %d, got %d", tc.size-1, chunks[len(chunks)-1].End)
			}

			var covered int64
			for i, c := range chunks {
				if c.End < c.Start {
					t.Fatalf("chunk %d has invalid range %d-%d", i, c.Start, c.End)
				}
				covered += c.End - c.Start + 1

				if i == 0 {
					continue
				}
				prev := chunks[i-1]
				// Adjacent boundaries prove no overlap and no hole.
				if c.Start != prev.End+1 {
					t.Fatalf("chunks %d and %d are not contiguous: prev=%d-%d cur=%d-%d", i-1, i, prev.Start, prev.End, c.Start, c.End)
				}
			}

			if covered != tc.size {
				t.Fatalf("covered bytes mismatch: got %d want %d", covered, tc.size)
			}
		})
	}
}

func TestSplitIntoChunks_InvalidInput(t *testing.T) {
	if got := splitIntoChunks(0, 4); got != nil {
		t.Fatalf("expected nil chunks for invalid size, got %#v", got)
	}
	if got := splitIntoChunks(10, 0); got != nil {
		t.Fatalf("expected nil chunks for invalid connection count, got %#v", got)
	}
}
