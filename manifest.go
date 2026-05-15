package main

import (
	"encoding/json"
	"fmt"
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

func manifestPath(outputPath string) string {
	return outputPath + ".fastget.json"
}

func saveManifest(path string, manifest *DownloadManifest) error {
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

func loadManifest(path string) (*DownloadManifest, error) {
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

func chunkSize(c Chunk) int64 {
	return c.End - c.Start + 1
}

func mergeRanges(ranges []ByteRange) []ByteRange {
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

func missingRanges(start, end int64, completed []ByteRange) []ByteRange {
	if end < start {
		return nil
	}

	merged := mergeRanges(completed)
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

func rangesCover(start, end int64, completed []ByteRange) bool {
	return len(missingRanges(start, end, completed)) == 0
}

func normalizeChunk(c *Chunk) {
	size := chunkSize(*c)
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

	c.CompletedRanges = mergeRanges(clamped)
	c.Done = rangesCover(c.Start, c.End, c.CompletedRanges)

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

func manifestTotals(manifest *DownloadManifest) (downloaded int64, total int64, doneChunks int) {
	for i := range manifest.Chunks {
		normalizeChunk(&manifest.Chunks[i])
		c := manifest.Chunks[i]
		downloaded += c.DownloadedBytes
		total += chunkSize(c)
		if c.Done {
			doneChunks++
		}
	}
	return downloaded, total, doneChunks
}

func manifestComplete(manifest *DownloadManifest) bool {
	if len(manifest.Chunks) == 0 {
		return false
	}
	_, _, doneChunks := manifestTotals(manifest)
	return doneChunks == len(manifest.Chunks)
}

func runStatus(outputPath string) error {
	path := manifestPath(outputPath)
	manifest, err := loadManifest(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("manifest not found: %s", path)
		}
		return err
	}

	downloaded, total, doneChunks := manifestTotals(manifest)
	percent := 0.0
	if total > 0 {
		percent = float64(downloaded) / float64(total) * 100
		if percent > 100 {
			percent = 100
		}
	}
	state := "incomplete"
	if doneChunks == len(manifest.Chunks) && len(manifest.Chunks) > 0 {
		state = "complete"
	}

	fmt.Println("Manifest:", path)
	fmt.Println("URL:", manifest.URL)
	fmt.Println("Output:", manifest.OutputPath)
	fmt.Println("Total size:", manifest.TotalSize)
	fmt.Println("Connections:", manifest.Connections)
	fmt.Println("Queue mode:", manifest.QueueMode)
	if manifest.QueueMode {
		fmt.Println("Segment size:", manifest.SegmentSize)
	}
	fmt.Printf("Chunks: %d/%d complete\n", doneChunks, len(manifest.Chunks))
	fmt.Printf("Progress: %d/%d bytes (%.2f%%)\n", downloaded, total, percent)
	fmt.Println("State:", state)

	fmt.Println()
	fmt.Println("Raw manifest JSON:")
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func runClean(outputPath string) error {
	path := manifestPath(outputPath)
	manifest, err := loadManifest(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Manifest not found:", path)
			fmt.Println("Output file unchanged:", outputPath)
			return nil
		}
		return err
	}

	complete := manifestComplete(manifest)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Println("Removed manifest:", path)

	if complete {
		fmt.Println("Download is complete; kept output file:", outputPath)
		return nil
	}

	if err := os.Remove(outputPath); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Partial output file already missing:", outputPath)
			return nil
		}
		return err
	}

	fmt.Println("Removed partial output file:", outputPath)
	return nil
}
