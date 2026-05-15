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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Constanst
// refreshInterval refresh terminal with stats every 200ms
const refreshInterval = 200

// saveRefreshInterval saves the manifest to disk every 5000ms (5s)
const saveRefreshInterval = 5000

type Chunk struct {
	Index           int
	Start           int64
	End             int64
	DownloadedBytes int64
	Done            bool
}

type DownloadManifest struct {
	URL         string  `json:"url"`
	OutputPath  string  `json:"output_path"`
	TotalSize   int64   `json:"total_size"`
	Connections int     `json:"connections"`
	Chunks      []Chunk `json:"chunks"`
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
		chunks = append(chunks, Chunk{
			Index:           i,
			Start:           start,
			End:             end,
			DownloadedBytes: 0,
			Done:            false,
		})
		start = end + 1
	}

	// Ensure the final chunk always ends exactly at size-1.
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

func chunkSize(c *Chunk) int64 {
	return c.End - c.Start + 1
}

func clampChunkProgress(c *Chunk) {
	size := chunkSize(c)
	if c.DownloadedBytes < 0 {
		c.DownloadedBytes = 0
	}
	if c.DownloadedBytes > size {
		c.DownloadedBytes = size
	}
	c.Done = c.DownloadedBytes >= size
}

func downloadChunk(rawURL string, outputPath string, chunk *Chunk, downloaded *int64, chunkDownloaded *int64, manifestMu *sync.Mutex) error {
	manifestMu.Lock()
	clampChunkProgress(chunk)
	currentDownloaded := chunk.DownloadedBytes
	chunkIsDone := chunk.Done
	manifestMu.Unlock()

	if chunkIsDone {
		return nil
	}

	size := chunkSize(chunk)
	if size <= 0 {
		return fmt.Errorf("invalid chunk size for chunk %d", chunk.Index)
	}

	resumeOffset := chunk.Start + currentDownloaded
	if resumeOffset > chunk.End {
		manifestMu.Lock()
		chunk.Done = true
		chunk.DownloadedBytes = size
		manifestMu.Unlock()
		return nil
	}

	// Byte-level resume first.
	startOffset := resumeOffset
	if err := downloadChunkRange(rawURL, outputPath, chunk, startOffset, downloaded, chunkDownloaded, manifestMu); err == nil {
		return nil
	} else {
		// Fallback to chunk-level resume only for this chunk.
		fmt.Printf("\nByte resume failed for chunk %d, restarting that chunk.\n", chunk.Index)
		manifestMu.Lock()
		chunk.DownloadedBytes = 0
		chunk.Done = false
		atomic.StoreInt64(chunkDownloaded, 0)
		manifestMu.Unlock()
		return downloadChunkRange(rawURL, outputPath, chunk, chunk.Start, downloaded, chunkDownloaded, manifestMu)
	}
}

func downloadChunkRange(rawURL string, outputPath string, chunk *Chunk, startOffset int64, downloaded *int64, chunkDownloaded *int64, manifestMu *sync.Mutex) error {
	if startOffset < chunk.Start || startOffset > chunk.End {
		return fmt.Errorf("invalid resume offset %d for chunk %d", startOffset, chunk.Index)
	}

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", startOffset, chunk.End))

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
	offset := startOffset

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			written, writeErr := file.WriteAt(buf[:n], offset)
			if writeErr != nil {
				return writeErr
			}
			if written < 0 {
				return fmt.Errorf("negative write length for chunk %d", chunk.Index)
			}
			offset += int64(written)
			atomic.AddInt64(downloaded, int64(written))
			atomic.AddInt64(chunkDownloaded, int64(written))
			manifestMu.Lock()
			chunk.DownloadedBytes += int64(written)
			clampChunkProgress(chunk)
			manifestMu.Unlock()
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	manifestMu.Lock()
	clampChunkProgress(chunk)
	manifestMu.Unlock()
	return nil
}

func downloadChunkWithRetry(rawURL string, outputPath string, chunk *Chunk, downloaded *int64, chunkDownloaded *int64, maxRetries int, manifestMu *sync.Mutex) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second)
		}

		err := downloadChunk(rawURL, outputPath, chunk, downloaded, chunkDownloaded, manifestMu)
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
		ticker := time.NewTicker(refreshInterval * time.Millisecond)
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
		ticker := time.NewTicker(refreshInterval * time.Millisecond)
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
				percent := float64(chunkVal) / float64(chunkTotals[i]) * 100
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

func downloadParallel(rawURL string, outputPath string, totalSize int64, connections int, verbose bool, maxRetries int) error {
	mPath := manifestPath(outputPath)
	var manifest DownloadManifest
	resuming := false

	existing, err := loadManifest(mPath)
	if err == nil &&
		existing.URL == rawURL &&
		existing.OutputPath == outputPath &&
		existing.TotalSize == totalSize &&
		existing.Connections == connections {
		manifest = *existing
		resuming = true
	} else {
		chunks := splitIntoChunks(totalSize, connections)
		if len(chunks) == 0 {
			return fmt.Errorf("cannot split file into chunks")
		}
		manifest = DownloadManifest{
			URL:         rawURL,
			OutputPath:  outputPath,
			TotalSize:   totalSize,
			Connections: connections,
			Chunks:      chunks,
		}
	}

	if !resuming {
		if err := preallocateFile(outputPath, totalSize); err != nil {
			return err
		}
		if err := saveManifest(mPath, &manifest); err != nil {
			return err
		}
	}

	chunks := manifest.Chunks
	if len(chunks) == 0 {
		return fmt.Errorf("cannot split file into chunks")
	}

	errChan := make(chan error, len(chunks))
	var downloaded int64
	chunkDownloaded := make([]int64, len(chunks))
	chunkTotals := make([]int64, len(chunks))
	var manifestMu sync.Mutex
	for i, c := range chunks {
		chunkTotals[i] = c.End - c.Start + 1
		clampChunkProgress(&chunks[i])
		chunkDownloaded[i] = chunks[i].DownloadedBytes
		atomic.AddInt64(&downloaded, chunks[i].DownloadedBytes)
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
		t := time.NewTicker(saveRefreshInterval * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				manifestMu.Lock()
				saveErr := saveManifest(mPath, &manifest)
				manifestMu.Unlock()
				if saveErr != nil {
					fmt.Printf("\nwarning: manifest save failed: %v\n", saveErr)
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

	var wg sync.WaitGroup
	for i := range chunks {
		if chunks[i].Done {
			continue
		}

		wg.Add(1)
		chunk := &chunks[i]
		go func() {
			defer wg.Done()
			if err := downloadChunkWithRetry(rawURL, outputPath, chunk, &downloaded, &chunkDownloaded[chunk.Index], maxRetries, &manifestMu); err != nil {
				errChan <- fmt.Errorf("chunk %d failed: %w", chunk.Index, err)
				return
			}

			manifestMu.Lock()
			clampChunkProgress(&manifest.Chunks[chunk.Index])
			saveErr := saveManifest(mPath, &manifest)
			manifestMu.Unlock()
			if saveErr != nil {
				errChan <- fmt.Errorf("chunk %d manifest save failed: %w", chunk.Index, saveErr)
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

	if err := os.Remove(mPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Println("Download completed successfully.")

	return nil
}

// getFileInfo performs a HEAD request to the given URL and returns the final URL after redirects,
// the content length (or -1 if not available), whether byte-range requests are supported, and any error encountered.
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
	workers := flag.Int("n", 8, "number of parallel connections")
	retries := flag.Int("retries", 3, "max retries per chunk")
	verbose := flag.Bool("v", false, "verbose progress output (includes per-chunk status)")
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

	if *out == "" {
		fmt.Fprintln(os.Stderr, "error: -o output filename is required")
		os.Exit(1)
	}

	fmt.Println("URL:", parsed.String())
	fmt.Println("Output:", *out)
	fmt.Println("Parallel connections:", *workers)

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
	if err := downloadParallel(finalURL, *out, size, *workers, *verbose, *retries); err != nil {
		fmt.Fprintf(os.Stderr, "error: parallel download failed: %v\n", err)
		os.Exit(1)
	}
}
