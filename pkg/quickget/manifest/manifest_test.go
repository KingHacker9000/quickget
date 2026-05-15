package manifest

import "testing"

func TestMissingRanges(t *testing.T) {
	got := MissingRanges(0, 9, []ByteRange{{Start: 0, End: 1}, {Start: 4, End: 6}})
	if len(got) != 2 {
		t.Fatalf("expected 2 missing ranges, got %d", len(got))
	}
	if got[0] != (ByteRange{Start: 2, End: 3}) || got[1] != (ByteRange{Start: 7, End: 9}) {
		t.Fatalf("unexpected missing ranges: %#v", got)
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
