package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// refreshIntervalMS controls how often the progress display is updated (in milliseconds).
	refreshIntervalMS = 200
	// DefaultParallelConnections is the default number of parallel connections to use for downloading.
	DefaultParallelConnections = 8
	// DefaultDynamicSplitting is the default value for dynamic splitting.
	DefaultDynamicSplitting = true
	// DefaultMinSplitSizeBytes is the minimum size of each split chunk.
	DefaultMinSplitSizeBytes = 8 * 1024 * 1024
	// DefaultMinDynamicFileSizeBytes is the minimum file size for dynamic splitting.
	DefaultMinDynamicFileSizeBytes = 64 * 1024 * 1024
	// jsonSaveInterval controls how often the download manifest is saved to disk during the download process.
	jsonSaveInterval = 1000
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
	Chunks      []Chunk `json:"chunks"`
}

type SegmentTask struct {
	ChunkIndex int
	Start      int64
	End        int64
}

func splitIntoChunks(size int64, connections int) []Chunk {
	if size <= 0 || connections <= 0 {
		return nil
	}

	chunks := make([]Chunk, 0, connections)
	baseSize := size / int64(connections)
	remainder := size % int64(connections)

	start := int64(0)
	for i := 0; i < connections; i++ {
		partSize := baseSize
		if int64(i) < remainder {
			partSize++
		}

		end := start + partSize - 1
		chunks = append(chunks, Chunk{Index: i, Start: start, End: end})
		start = end + 1
	}

	if len(chunks) > 0 {
		chunks[len(chunks)-1].End = size - 1
	}

	return chunks
}

func preallocateFile(outputPath string, size int64) error {
	if size < 0 {
		return fmt.Errorf("invalid file size: %d", size)
	}

	f, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	return f.Truncate(size)
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

func downloadSegment(rawURL string, outputPath string, task SegmentTask, downloaded *int64, chunkDownloaded *int64) error {
	if task.End < task.Start {
		return fmt.Errorf("invalid range %d-%d", task.Start, task.End)
	}

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", task.Start, task.End))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("expected 206 Partial Content, got %s", resp.Status)
	}

	file, err := os.OpenFile(outputPath, os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	buf := make([]byte, 256*1024)
	offset := task.Start

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			written, writeErr := file.WriteAt(buf[:n], offset)
			if writeErr != nil {
				return writeErr
			}
			if written <= 0 {
				return fmt.Errorf("invalid write length")
			}
			offset += int64(written)
			atomic.AddInt64(downloaded, int64(written))
			atomic.AddInt64(chunkDownloaded, int64(written))
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	return nil
}

func downloadSegmentWithRetry(rawURL string, outputPath string, task SegmentTask, downloaded *int64, chunkDownloaded *int64, maxRetries int) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second)
		}
		err := downloadSegment(rawURL, outputPath, task, downloaded, chunkDownloaded)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}

func printProgress(downloaded, total int64, start time.Time) {
	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	downloadedMB := float64(downloaded) / 1024 / 1024
	speedMBps := downloadedMB / elapsed

	if total > 0 {
		percent := float64(downloaded) / float64(total) * 100
		if percent > 100 {
			percent = 100
		}
		barWidth := 30
		filled := int(percent / 100 * float64(barWidth))
		if filled < 0 {
			filled = 0
		}
		if filled > barWidth {
			filled = barWidth
		}
		bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
		fmt.Printf("\r[%s] %5.1f%% %.2f MB / %.2f MB %.2f MB/s", bar, percent, downloadedMB, float64(total)/1024/1024, speedMBps)
		return
	}

	fmt.Printf("\r[%-30s]   ??.?%% %.2f MB / ? MB %.2f MB/s", "", downloadedMB, speedMBps)
}

func startProgressLoop(downloaded *int64, totalSize int64) (chan struct{}, chan struct{}) {
	done := make(chan struct{})
	finished := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(finished)
		ticker := time.NewTicker(refreshIntervalMS * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				current := atomic.LoadInt64(downloaded)
				printProgress(current, totalSize, start)
			case <-done:
				current := atomic.LoadInt64(downloaded)
				printProgress(current, totalSize, start)
				fmt.Println()
				return
			}
		}
	}()
	return done, finished
}

func startVerboseProgressLoop(downloaded *int64, totalSize int64, chunkTotals []int64, chunkDownloaded []int64) (chan struct{}, chan struct{}) {
	done := make(chan struct{})
	finished := make(chan struct{})
	start := time.Now()

	go func() {
		defer close(finished)
		ticker := time.NewTicker(refreshIntervalMS * time.Millisecond)
		defer ticker.Stop()
		lines := len(chunkTotals) + 1
		firstRender := true

		render := func(final bool) {
			if !firstRender {
				fmt.Printf("\033[%dA", lines)
			}

			currentTotal := atomic.LoadInt64(downloaded)
			elapsed := time.Since(start).Seconds()
			if elapsed <= 0 {
				elapsed = 1
			}
			totalPercent := float64(currentTotal) / float64(totalSize) * 100
			if totalPercent > 100 {
				totalPercent = 100
			}
			totalMB := float64(currentTotal) / 1024 / 1024
			speedMBps := totalMB / elapsed
			fmt.Printf("[TOTAL] %5.1f%% %.2f/%.2f MB %.2f MB/s\n", totalPercent, totalMB, float64(totalSize)/1024/1024, speedMBps)

			for i := range chunkTotals {
				chunkVal := atomic.LoadInt64(&chunkDownloaded[i])
				percent := 0.0
				if chunkTotals[i] > 0 {
					percent = float64(chunkVal) / float64(chunkTotals[i]) * 100
				}
				if percent > 100 {
					percent = 100
				}
				barWidth := 24
				filled := int(percent / 100 * float64(barWidth))
				if filled < 0 {
					filled = 0
				}
				if filled > barWidth {
					filled = barWidth
				}
				bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
				fmt.Printf("[C%02d] [%s] %5.1f%% %.2f/%.2f MB\n", i, bar, percent, float64(chunkVal)/1024/1024, float64(chunkTotals[i])/1024/1024)
			}

			firstRender = false
			if final {
				fmt.Println()
			}
		}

		for {
			select {
			case <-ticker.C:
				render(false)
			case <-done:
				render(true)
				return
			}
		}
	}()

	return done, finished
}

func downloadParallel(rawURL string, outputPath string, totalSize int64, connections int, verbose bool, maxRetries int, dynamic bool, minSplitSize int64, minDynamicFileSize int64) error {
	mPath := manifestPath(outputPath)
	var manifest DownloadManifest

	existing, err := loadManifest(mPath)
	if err == nil &&
		existing.URL == rawURL &&
		existing.OutputPath == outputPath &&
		existing.TotalSize == totalSize &&
		existing.Connections == connections {
		manifest = *existing
	} else {
		chunks := splitIntoChunks(totalSize, connections)
		if len(chunks) == 0 {
			return fmt.Errorf("cannot split file into chunks")
		}
		manifest = DownloadManifest{URL: rawURL, OutputPath: outputPath, TotalSize: totalSize, Connections: connections, Chunks: chunks}
		if err := preallocateFile(outputPath, totalSize); err != nil {
			return err
		}
		if err := saveManifest(mPath, &manifest); err != nil {
			return err
		}
	}

	if dynamic && totalSize < minDynamicFileSize {
		fmt.Printf("Dynamic splitting disabled for this file size (size=%d, min=%d).\n", totalSize, minDynamicFileSize)
		dynamic = false
	}

	for i := range manifest.Chunks {
		normalizeChunk(&manifest.Chunks[i])
	}

	var manifestMu sync.Mutex
	remaining := make(map[int][]ByteRange)
	for i, c := range manifest.Chunks {
		if c.Done {
			continue
		}
		missing := missingRanges(c.Start, c.End, c.CompletedRanges)
		if len(missing) > 0 {
			remaining[i] = missing
		}
	}

	var downloaded int64
	chunkDownloaded := make([]int64, len(manifest.Chunks))
	chunkTotals := make([]int64, len(manifest.Chunks))
	for i, c := range manifest.Chunks {
		chunkDownloaded[i] = c.DownloadedBytes
		chunkTotals[i] = chunkSize(c)
		atomic.AddInt64(&downloaded, c.DownloadedBytes)
	}

	var progressDone chan struct{}
	var progressFinished chan struct{}
	if verbose {
		progressDone, progressFinished = startVerboseProgressLoop(&downloaded, totalSize, chunkTotals, chunkDownloaded)
	} else {
		progressDone, progressFinished = startProgressLoop(&downloaded, totalSize)
	}

	stopSaver := make(chan struct{})
	saverDone := make(chan struct{})
	go func() {
		defer close(saverDone)
		t := time.NewTicker(jsonSaveInterval * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				manifestMu.Lock()
				err := saveManifest(mPath, &manifest)
				manifestMu.Unlock()
				if err != nil {
					fmt.Printf("\nwarning: manifest save failed: %v\n", err)
				}
			case <-stopSaver:
				return
			}
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		manifestMu.Lock()
		_ = saveManifest(mPath, &manifest)
		manifestMu.Unlock()
		os.Exit(130)
	}()

	pickTask := func() (SegmentTask, bool) {
		manifestMu.Lock()
		defer manifestMu.Unlock()

		maxChunk := -1
		maxRangeIdx := -1
		maxLen := int64(0)
		var maxRange ByteRange

		for ci, ranges := range remaining {
			for ri, r := range ranges {
				l := r.End - r.Start + 1
				if l > maxLen {
					maxLen = l
					maxChunk = ci
					maxRangeIdx = ri
					maxRange = r
				}
			}
		}

		if maxChunk == -1 {
			return SegmentTask{}, false
		}

		assign := maxRange
		if dynamic && maxLen >= minSplitSize*2 {
			assignSize := maxLen / 2
			assign = ByteRange{Start: maxRange.Start, End: maxRange.Start + assignSize - 1}
			remaining[maxChunk][maxRangeIdx].Start = assign.End + 1
		} else {
			last := len(remaining[maxChunk]) - 1
			remaining[maxChunk][maxRangeIdx] = remaining[maxChunk][last]
			remaining[maxChunk] = remaining[maxChunk][:last]
			if len(remaining[maxChunk]) == 0 {
				delete(remaining, maxChunk)
			}
		}

		return SegmentTask{ChunkIndex: maxChunk, Start: assign.Start, End: assign.End}, true
	}

	errChan := make(chan error, connections)
	var wg sync.WaitGroup
	for i := 0; i < connections; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				task, ok := pickTask()
				if !ok {
					return
				}

				if err := downloadSegmentWithRetry(rawURL, outputPath, task, &downloaded, &chunkDownloaded[task.ChunkIndex], maxRetries); err != nil {
					errChan <- fmt.Errorf("chunk %d segment %d-%d failed: %w", task.ChunkIndex, task.Start, task.End, err)
					return
				}

				manifestMu.Lock()
				c := &manifest.Chunks[task.ChunkIndex]
				c.CompletedRanges = append(c.CompletedRanges, ByteRange{Start: task.Start, End: task.End})
				normalizeChunk(c)
				saveErr := saveManifest(mPath, &manifest)
				manifestMu.Unlock()
				if saveErr != nil {
					errChan <- fmt.Errorf("chunk %d manifest save failed: %w", task.ChunkIndex, saveErr)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(stopSaver)
	<-saverDone
	close(progressDone)
	<-progressFinished
	close(errChan)

	for err := range errChan {
		if err != nil {
			return err
		}
	}

	manifestMu.Lock()
	_ = saveManifest(mPath, &manifest)
	manifestMu.Unlock()
	if err := os.Remove(mPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Println("Download completed successfully.")
	return nil
}

func getFileInfo(rawURL string) (finalURL string, size int64, rangeSupported bool, err error) {
	size = -1

	req, err := http.NewRequest(http.MethodHead, rawURL, nil)
	if err != nil {
		return "", -1, false, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", -1, false, err
	}
	defer resp.Body.Close()

	finalURL = resp.Request.URL.String()

	contentLength := strings.TrimSpace(resp.Header.Get("Content-Length"))
	if contentLength != "" {
		if v, parseErr := strconv.ParseInt(contentLength, 10, 64); parseErr == nil {
			size = v
		}
	}

	acceptRanges := strings.ToLower(resp.Header.Get("Accept-Ranges"))
	rangeSupported = strings.Contains(acceptRanges, "bytes")

	return finalURL, size, rangeSupported, nil
}

func downloadSingle(rawURL string, outputPath string, totalSize int64, verbose bool) error {
	resp, err := http.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET failed with status: %s", resp.Status)
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()

	var downloaded int64
	progressDone, progressFinished := startProgressLoop(&downloaded, totalSize)
	defer func() {
		close(progressDone)
		<-progressFinished
	}()

	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			written, writeErr := out.Write(buf[:n])
			if writeErr != nil {
				return writeErr
			}
			atomic.AddInt64(&downloaded, int64(written))
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	return nil
}

func main() {
	out := flag.String("o", "", "output filename")
	workers := flag.Int("n", DefaultParallelConnections, "number of parallel connections")
	retries := flag.Int("retries", 3, "max retries per chunk")
	verbose := flag.Bool("v", false, "verbose progress output (includes per-chunk status)")
	dynamic := flag.Bool("d", DefaultDynamicSplitting, "enable dynamic chunk splitting/work stealing")
	minSplitSize := flag.Int64("min-split-size", DefaultMinSplitSizeBytes, "minimum range size (bytes) required before dynamic split")
	minDynamicFileSize := flag.Int64("min-dynamic-file-size", DefaultMinDynamicFileSizeBytes, "minimum file size (bytes) required to enable dynamic splitting")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] <url>\n\n", os.Args[0])
		fmt.Fprintln(flag.CommandLine.Output(), "Options:")
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "error: exactly one positional URL argument is required")
		flag.Usage()
		os.Exit(1)
	}

	rawURL := flag.Arg(0)
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		fmt.Fprintf(os.Stderr, "error: invalid URL: %q\n", rawURL)
		os.Exit(1)
	}
	if *workers <= 0 {
		fmt.Fprintln(os.Stderr, "error: -n must be greater than 0")
		os.Exit(1)
	}
	if *retries < 0 {
		fmt.Fprintln(os.Stderr, "error: -retries must be >= 0")
		os.Exit(1)
	}
	if *minSplitSize <= 0 {
		fmt.Fprintln(os.Stderr, "error: -min-split-size must be > 0")
		os.Exit(1)
	}
	if *minDynamicFileSize <= 0 {
		fmt.Fprintln(os.Stderr, "error: -min-dynamic-file-size must be > 0")
		os.Exit(1)
	}
	if *out == "" {
		fmt.Fprintln(os.Stderr, "error: -o output filename is required")
		os.Exit(1)
	}

	fmt.Println("URL:", parsed.String())
	fmt.Println("Output:", *out)
	fmt.Println("Parallel connections:", *workers)
	fmt.Println("Dynamic splitting:", *dynamic)

	finalURL, size, rangeSupported, err := getFileInfo(parsed.String())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: HEAD request failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Final URL:", finalURL)
	fmt.Println("File size:", size)
	fmt.Println("Range supported:", rangeSupported)

	chunks := splitIntoChunks(size, *workers)
	if len(chunks) == 0 {
		fmt.Println("Chunks: unavailable (file size unknown or invalid)")
	} else {
		fmt.Println("Chunks:")
		for _, c := range chunks {
			fmt.Printf("  [%d] %d-%d\n", c.Index, c.Start, c.End)
		}
	}

	useSingle := size <= 0 || !rangeSupported || *workers <= 1
	if useSingle {
		fmt.Println("Mode: single download")
		if err := downloadSingle(finalURL, *out, size, *verbose); err != nil {
			fmt.Fprintf(os.Stderr, "error: download failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	fmt.Println("Mode: parallel download")
	if err := downloadParallel(finalURL, *out, size, *workers, *verbose, *retries, *dynamic, *minSplitSize, *minDynamicFileSize); err != nil {
		fmt.Fprintf(os.Stderr, "error: parallel download failed: %v\n", err)
		os.Exit(1)
	}
}
