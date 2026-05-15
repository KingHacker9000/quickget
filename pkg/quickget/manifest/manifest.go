package manifest

import (
	"encoding/json"
	"os"
	"sort"
)

type ByteRange struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

type Chunk struct {
	Index           int         `json:"index"`
	Start           int64       `json:"start"`
	End             int64       `json:"end"`
	DownloadedBytes int64       `json:"downloaded_bytes,omitempty"`
	Done            bool        `json:"done"`
	CompletedRanges []ByteRange `json:"completed_ranges"`
}

type DownloadManifest struct {
	URL         string  `json:"url"`
	OutputPath  string  `json:"output_path"`
	TotalSize   int64   `json:"total_size"`
	Connections int     `json:"connections"`
	QueueMode   bool    `json:"queue_mode,omitempty"`
	SegmentSize int64   `json:"segment_size,omitempty"`
	Chunks      []Chunk `json:"chunks"`
}

type SegmentTask struct {
	ChunkIndex int
	Start      int64
	End        int64
}

func Path(outputPath string) string {
	return outputPath + ".quickget.json"
}

func Save(path string, m *DownloadManifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

func Load(path string) (*DownloadManifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m DownloadManifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func ChunkSize(c Chunk) int64 {
	return c.End - c.Start + 1
}

func MergeRanges(ranges []ByteRange) []ByteRange {
	if len(ranges) == 0 {
		return nil
	}

	filtered := make([]ByteRange, 0, len(ranges))
	for _, r := range ranges {
		if r.End >= r.Start {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Start == filtered[j].Start {
			return filtered[i].End < filtered[j].End
		}
		return filtered[i].Start < filtered[j].Start
	})

	merged := []ByteRange{filtered[0]}
	for i := 1; i < len(filtered); i++ {
		cur := filtered[i]
		last := &merged[len(merged)-1]
		if cur.Start <= last.End+1 {
			if cur.End > last.End {
				last.End = cur.End
			}
			continue
		}
		merged = append(merged, cur)
	}

	return merged
}

func MissingRanges(start, end int64, completed []ByteRange) []ByteRange {
	if end < start {
		return nil
	}

	merged := MergeRanges(completed)
	if len(merged) == 0 {
		return []ByteRange{{Start: start, End: end}}
	}

	out := make([]ByteRange, 0)
	cursor := start
	for _, r := range merged {
		if r.End < start || r.Start > end {
			continue
		}
		rs := r.Start
		re := r.End
		if rs < start {
			rs = start
		}
		if re > end {
			re = end
		}
		if cursor < rs {
			out = append(out, ByteRange{Start: cursor, End: rs - 1})
		}
		if re+1 > cursor {
			cursor = re + 1
		}
		if cursor > end {
			break
		}
	}
	if cursor <= end {
		out = append(out, ByteRange{Start: cursor, End: end})
	}

	return out
}

func RangesCover(start, end int64, completed []ByteRange) bool {
	return len(MissingRanges(start, end, completed)) == 0
}

func NormalizeChunk(c *Chunk) {
	size := ChunkSize(*c)
	if size <= 0 {
		c.CompletedRanges = nil
		c.DownloadedBytes = 0
		c.Done = true
		return
	}

	if len(c.CompletedRanges) == 0 && c.DownloadedBytes > 0 {
		end := c.Start + c.DownloadedBytes - 1
		if end > c.End {
			end = c.End
		}
		if end >= c.Start {
			c.CompletedRanges = []ByteRange{{Start: c.Start, End: end}}
		}
	}

	clamped := make([]ByteRange, 0, len(c.CompletedRanges))
	for _, r := range c.CompletedRanges {
		rs := r.Start
		re := r.End
		if re < c.Start || rs > c.End {
			continue
		}
		if rs < c.Start {
			rs = c.Start
		}
		if re > c.End {
			re = c.End
		}
		if re >= rs {
			clamped = append(clamped, ByteRange{Start: rs, End: re})
		}
	}

	c.CompletedRanges = MergeRanges(clamped)
	c.Done = RangesCover(c.Start, c.End, c.CompletedRanges)

	total := int64(0)
	for _, r := range c.CompletedRanges {
		total += r.End - r.Start + 1
	}
	if total < 0 {
		total = 0
	}
	if total > size {
		total = size
	}
	c.DownloadedBytes = total
	if total == size {
		c.Done = true
	}
}

func Totals(m *DownloadManifest) (downloaded int64, total int64, doneChunks int) {
	for i := range m.Chunks {
		NormalizeChunk(&m.Chunks[i])
		c := m.Chunks[i]
		downloaded += c.DownloadedBytes
		total += ChunkSize(c)
		if c.Done {
			doneChunks++
		}
	}
	return downloaded, total, doneChunks
}

func Complete(m *DownloadManifest) bool {
	if len(m.Chunks) == 0 {
		return false
	}
	_, _, doneChunks := Totals(m)
	return doneChunks == len(m.Chunks)
}
