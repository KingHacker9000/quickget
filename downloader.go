package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	DefaultParallelConnections     = 8
	DefaultDynamicSplitting        = true
	DefaultMinSplitSizeBytes       = 32 * 1024 * 1024
	DefaultMinDynamicFileSizeBytes = 64 * 1024 * 1024
	DefaultSegmentSizeBytes        = 16 * 1024 * 1024
	DefaultBufferSizeBytes         = 1024 * 1024
	DefaultMaxIdleConns            = 1024
	DefaultIdleTimeoutSec          = 90
	DefaultForceHTTP1              = true
	jsonSaveInterval               = 5000
)

type downloadOptions struct {
	outputPath         string
	workers            int
	retries            int
	verbose            bool
	dynamic            bool
	minSplitSize       int64
	minDynamicFileSize int64
	queueMode          bool
	segmentSize        int64
	bufferSize         int
	bufferSizeSet      bool
	autoBuffer         bool
	forceHTTP1         bool
	maxIdleConns       int
	idleTimeoutSec     int
	writeDisk          string
	url                string
}

func runDownload(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		printDownloadUsage()
		return nil
	}

	normalized, err := normalizeDownloadArgs(args)
	if err != nil {
		printDownloadUsage()
		return err
	}

	options, err := parseDownloadOptions(normalized)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	validatedURL, err := validateURL(options.url)
	if err != nil {
		return err
	}

	client := newHTTPClient(options.workers, options.forceHTTP1, options.maxIdleConns, options.idleTimeoutSec)
	if options.autoBuffer && !options.bufferSizeSet {
		rec, err := RecommendBufferSizeForPath(options.outputPath)
		if err != nil {
			return fmt.Errorf("auto buffer tune failed: %w", err)
		}
		options.bufferSize = rec.BufferSize
		fmt.Printf("Auto buffer selected: %s (%d bytes)\n", formatBytesBinary(rec.BufferSize), rec.BufferSize)
	}

	fmt.Println("URL:", validatedURL)
	fmt.Println("Output:", options.outputPath)
	fmt.Println("Parallel connections:", options.workers)
	fmt.Println("Dynamic splitting:", options.dynamic)
	fmt.Println("Queue mode:", options.queueMode)
	if options.queueMode {
		fmt.Println("Segment size:", options.segmentSize)
	}

	info, err := fetchURLInfo(client, validatedURL)
	if err != nil {
		return fmt.Errorf("HEAD request failed: %w", err)
	}

	fmt.Println("Final URL:", info.FinalURL)
	fmt.Println("File size:", info.Size)
	fmt.Println("Range supported:", info.RangeSupported)

	if options.queueMode {
		segments := splitIntoSegments(info.Size, options.segmentSize)
		if len(segments) == 0 {
			fmt.Println("Segments: unavailable (file size unknown or invalid)")
		} else {
			fmt.Printf("Segments: %d jobs\n", len(segments))
		}
	} else {
		chunks := splitIntoChunks(info.Size, options.workers)
		if len(chunks) == 0 {
			fmt.Println("Chunks: unavailable (file size unknown or invalid)")
		} else {
			fmt.Println("Chunks:")
			for _, c := range chunks {
				fmt.Printf("  [%d] %d-%d\n", c.Index, c.Start, c.End)
			}
		}
	}

	useSingle := info.Size <= 0 || !info.RangeSupported || options.workers <= 1
	if useSingle {
		fmt.Println("Mode: single download")
		if err := downloadSingle(client, info.FinalURL, options.outputPath, info.Size, options.bufferSize); err != nil {
			return fmt.Errorf("download failed: %w", err)
		}
		return nil
	}

	fmt.Println("Mode: parallel download")
	if err := downloadParallel(client, info.FinalURL, options.outputPath, info.Size, options.workers, options.verbose, options.retries, options.dynamic, options.minSplitSize, options.minDynamicFileSize, options.queueMode, options.segmentSize, options.bufferSize, options.writeDisk); err != nil {
		return fmt.Errorf("parallel download failed: %w", err)
	}

	return nil
}

func parseDownloadOptions(args []string) (downloadOptions, error) {
	var opts downloadOptions

	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	output := fs.String("o", "", "output filename")
	workers := fs.Int("n", DefaultParallelConnections, "number of parallel connections")
	retries := fs.Int("retries", 3, "max retries per chunk")
	verbose := fs.Bool("v", false, "verbose progress output (includes per-chunk status)")
	dynamic := fs.Bool("d", DefaultDynamicSplitting, "enable dynamic chunk splitting/work stealing")
	minSplitSize := fs.Int64("min-split-size", DefaultMinSplitSizeBytes, "minimum range size (bytes) required before dynamic split")
	minDynamicFileSize := fs.Int64("min-dynamic-file-size", DefaultMinDynamicFileSizeBytes, "minimum file size (bytes) required to enable dynamic splitting")
	queueMode := fs.Bool("queue-mode", false, "enable queue-based segmented downloading")
	segmentSize := fs.Int64("segment-size", DefaultSegmentSizeBytes, "segment size in bytes used by queue mode")
	bufferSize := fs.Int("buffer-size", DefaultBufferSizeBytes, "download buffer size in bytes")
	autoBuffer := fs.Bool("auto-buffer", false, "auto-tune buffer size for output disk before download")
	forceHTTP1 := fs.Bool("http1", DefaultForceHTTP1, "disable HTTP/2 and force HTTP/1.1 behavior")
	maxIdleConns := fs.Int("max-idle-conns", DefaultMaxIdleConns, "max idle connections globally")
	idleTimeoutSec := fs.Int("idle-timeout", DefaultIdleTimeoutSec, "idle connection timeout in seconds")
	writeDisk := fs.String("write-disk", "", "disk/volume to measure write time for (e.g. C:)")

	fs.Usage = printDownloadUsage

	if err := fs.Parse(args); err != nil {
		return opts, err
	}

	if fs.NArg() != 1 {
		printDownloadUsage()
		return opts, errors.New("exactly one positional URL argument is required")
	}
	if *output == "" {
		return opts, errors.New("-o output filename is required")
	}
	if *workers <= 0 {
		return opts, errors.New("-n must be greater than 0")
	}
	if *retries < 0 {
		return opts, errors.New("-retries must be >= 0")
	}
	if *minSplitSize <= 0 {
		return opts, errors.New("-min-split-size must be > 0")
	}
	if *minDynamicFileSize <= 0 {
		return opts, errors.New("-min-dynamic-file-size must be > 0")
	}
	if *segmentSize <= 0 {
		return opts, errors.New("-segment-size must be > 0")
	}
	if *bufferSize <= 0 {
		return opts, errors.New("-buffer-size must be > 0")
	}
	if *maxIdleConns <= 0 {
		return opts, errors.New("-max-idle-conns must be > 0")
	}
	if *idleTimeoutSec <= 0 {
		return opts, errors.New("-idle-timeout must be > 0")
	}
	bufferSizeSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "buffer-size" {
			bufferSizeSet = true
		}
	})

	opts = downloadOptions{
		outputPath:         *output,
		workers:            *workers,
		retries:            *retries,
		verbose:            *verbose,
		dynamic:            *dynamic,
		minSplitSize:       *minSplitSize,
		minDynamicFileSize: *minDynamicFileSize,
		queueMode:          *queueMode,
		segmentSize:        *segmentSize,
		bufferSize:         *bufferSize,
		bufferSizeSet:      bufferSizeSet,
		autoBuffer:         *autoBuffer,
		forceHTTP1:         *forceHTTP1,
		maxIdleConns:       *maxIdleConns,
		idleTimeoutSec:     *idleTimeoutSec,
		writeDisk:          strings.TrimSpace(*writeDisk),
		url:                fs.Arg(0),
	}

	return opts, nil
}

func printDownloadUsage() {
	fmt.Fprintf(os.Stderr, "Usage: %s download [options] <url>\n", os.Args[0])
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Backward compatibility:")
	fmt.Fprintf(os.Stderr, "  %s [options] <url>\n", os.Args[0])
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Download options:")
	fmt.Fprintln(os.Stderr, "  -o string")
	fmt.Fprintln(os.Stderr, "        output filename (required)")
	fmt.Fprintf(os.Stderr, "  -n int\n        number of parallel connections (default %d)\n", DefaultParallelConnections)
	fmt.Fprintln(os.Stderr, "  -retries int")
	fmt.Fprintln(os.Stderr, "        max retries per chunk (default 3)")
	fmt.Fprintln(os.Stderr, "  -v")
	fmt.Fprintln(os.Stderr, "        verbose progress output (includes per-chunk status)")
	fmt.Fprintf(os.Stderr, "  -d\n        enable dynamic chunk splitting/work stealing (default %t)\n", DefaultDynamicSplitting)
	fmt.Fprintf(os.Stderr, "  -min-split-size int\n        minimum range size in bytes before dynamic split (default %d)\n", DefaultMinSplitSizeBytes)
	fmt.Fprintf(os.Stderr, "  -min-dynamic-file-size int\n        minimum file size in bytes required for dynamic splitting (default %d)\n", DefaultMinDynamicFileSizeBytes)
	fmt.Fprintln(os.Stderr, "  -queue-mode")
	fmt.Fprintln(os.Stderr, "        enable queue-based segmented downloading (default false)")
	fmt.Fprintf(os.Stderr, "  -segment-size int\n        segment size in bytes for queue mode (default %d)\n", DefaultSegmentSizeBytes)
	fmt.Fprintf(os.Stderr, "  -buffer-size int\n        download buffer size in bytes (default %d)\n", DefaultBufferSizeBytes)
	fmt.Fprintln(os.Stderr, "  -auto-buffer")
	fmt.Fprintln(os.Stderr, "        auto-tune buffer size for output disk before download")
	fmt.Fprintf(os.Stderr, "  -http1\n        disable HTTP/2 and force HTTP/1.1 behavior (default %t)\n", DefaultForceHTTP1)
	fmt.Fprintf(os.Stderr, "  -max-idle-conns int\n        max idle connections globally (default %d)\n", DefaultMaxIdleConns)
	fmt.Fprintf(os.Stderr, "  -idle-timeout int\n        idle connection timeout in seconds (default %d)\n", DefaultIdleTimeoutSec)
	fmt.Fprintln(os.Stderr, "  -write-disk string")
	fmt.Fprintln(os.Stderr, "        disk/volume to measure write timing for (example: C:)")
}

func normalizeDownloadArgs(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, errors.New("missing URL")
	}

	boolFlags := map[string]bool{
		"v":           true,
		"d":           true,
		"http1":       true,
		"auto-buffer": true,
	}
	valueFlags := map[string]bool{
		"o":                     true,
		"n":                     true,
		"retries":               true,
		"min-split-size":        true,
		"min-dynamic-file-size": true,
		"segment-size":          true,
		"buffer-size":           true,
		"max-idle-conns":        true,
		"idle-timeout":          true,
		"write-disk":            true,
	}
	boolFlags["queue-mode"] = true

	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, 2)

	for i := 0; i < len(args); i++ {
		tok := args[i]
		if tok == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if strings.HasPrefix(tok, "-") && tok != "-" {
			name, hasValue := splitFlagName(tok)
			flags = append(flags, tok)
			if hasValue {
				continue
			}
			if valueFlags[name] {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("flag -%s requires a value", name)
				}
				i++
				flags = append(flags, args[i])
				continue
			}
			if boolFlags[name] {
				continue
			}
			continue
		}
		positionals = append(positionals, tok)
	}

	if len(positionals) != 1 {
		return nil, fmt.Errorf("expected exactly one URL, got %d", len(positionals))
	}

	normalized := append(flags, positionals[0])
	return normalized, nil
}

func splitFlagName(flagToken string) (string, bool) {
	trimmed := strings.TrimLeft(flagToken, "-")
	if trimmed == "" {
		return "", false
	}
	parts := strings.SplitN(trimmed, "=", 2)
	if len(parts) == 2 {
		return parts[0], true
	}
	return trimmed, false
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

func newHTTPClient(connections int, forceHTTP1 bool, maxIdleConns int, idleTimeoutSec int) *http.Client {
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:   !forceHTTP1,
		MaxIdleConns:        maxIdleConns,
		MaxIdleConnsPerHost: connections,
		MaxConnsPerHost:     connections,
		IdleConnTimeout:     time.Duration(idleTimeoutSec) * time.Second,
		DisableCompression:  true,
	}
	if forceHTTP1 {
		transport.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
	}

	return &http.Client{Transport: transport}
}

func downloadSegment(client *http.Client, rawURL string, outputPath string, task SegmentTask, downloaded *int64, chunkDownloaded *int64, bufPool *sync.Pool, stats *DownloadStats) error {
	if task.End < task.Start {
		return fmt.Errorf("invalid range %d-%d", task.Start, task.End)
	}

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", task.Start, task.End))

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	byteRange := fmt.Sprintf("bytes=%d-%d", task.Start, task.End)
	if err := explainServerStatus(resp.StatusCode, true, byteRange, rawURL); err != nil {
		return err
	}

	file, err := os.OpenFile(outputPath, os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	buf := bufPool.Get().([]byte)
	defer bufPool.Put(buf)
	offset := task.Start

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			writeStart := time.Now()
			written, writeErr := file.WriteAt(buf[:n], offset)
			if stats != nil {
				stats.ObserveWrite(time.Since(writeStart))
			}
			if writeErr != nil {
				return writeErr
			}
			if written <= 0 {
				return errors.New("invalid write length")
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

func downloadSegmentWithRetry(client *http.Client, rawURL string, outputPath string, task SegmentTask, downloaded *int64, chunkDownloaded *int64, maxRetries int, bufPool *sync.Pool, stats *DownloadStats) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second)
		}
		err := downloadSegment(client, rawURL, outputPath, task, downloaded, chunkDownloaded, bufPool, stats)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}

func downloadSingle(client *http.Client, rawURL string, outputPath string, totalSize int64, bufferSize int) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
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

	buf := make([]byte, bufferSize)
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

func splitIntoSegments(size int64, segmentSize int64) []Chunk {
	if size <= 0 || segmentSize <= 0 {
		return nil
	}
	chunks := make([]Chunk, 0, (size+segmentSize-1)/segmentSize)
	var index int
	for start := int64(0); start < size; start += segmentSize {
		end := start + segmentSize - 1
		if end >= size {
			end = size - 1
		}
		chunks = append(chunks, Chunk{Index: index, Start: start, End: end})
		index++
	}
	return chunks
}

func downloadParallel(client *http.Client, rawURL string, outputPath string, totalSize int64, connections int, verbose bool, maxRetries int, dynamic bool, minSplitSize int64, minDynamicFileSize int64, queueMode bool, segmentSize int64, bufferSize int, writeDisk string) error {
	mPath := manifestPath(outputPath)
	var manifest DownloadManifest

	existing, err := loadManifest(mPath)
	if err == nil &&
		existing.URL == rawURL &&
		existing.OutputPath == outputPath &&
		existing.TotalSize == totalSize &&
		existing.Connections == connections &&
		existing.QueueMode == queueMode &&
		(!queueMode || existing.SegmentSize == segmentSize) {
		manifest = *existing
	} else {
		var chunks []Chunk
		if queueMode {
			chunks = splitIntoSegments(totalSize, segmentSize)
		} else {
			chunks = splitIntoChunks(totalSize, connections)
		}
		if len(chunks) == 0 {
			return errors.New("cannot split file into chunks")
		}
		manifest = DownloadManifest{
			URL:         rawURL,
			OutputPath:  outputPath,
			TotalSize:   totalSize,
			Connections: connections,
			QueueMode:   queueMode,
			SegmentSize: segmentSize,
			Chunks:      chunks,
		}
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
	bufPool := &sync.Pool{
		New: func() interface{} {
			return make([]byte, bufferSize)
		},
	}
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
	var writeStats *DownloadStats
	measureWrites := shouldMeasureWriteDisk(outputPath, writeDisk)
	if measureWrites {
		writeStats = NewDownloadStats()
	}
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
		progressDone, progressFinished = startVerboseProgressLoop(&downloaded, totalSize, chunkTotals, chunkDownloaded, writeStats)
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

	if queueMode {
		return runQueueMode(client, rawURL, outputPath, connections, maxRetries, mPath, &manifest, &manifestMu, &downloaded, chunkDownloaded, stopSaver, saverDone, progressDone, progressFinished, bufPool, writeStats)
	}

	pickTask := func() (SegmentTask, bool) {
		manifestMu.Lock()
		defer manifestMu.Unlock()

		maxChunk := -1
		maxRangeIdx := -1
		maxLen := int64(0)
		var maxRange ByteRange

		for ci, ranges := range remaining {
			for ri, r := range ranges {
				length := r.End - r.Start + 1
				if length > maxLen {
					maxLen = length
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

				if err := downloadSegmentWithRetry(client, rawURL, outputPath, task, &downloaded, &chunkDownloaded[task.ChunkIndex], maxRetries, bufPool, writeStats); err != nil {
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

	for runErr := range errChan {
		if runErr != nil {
			return runErr
		}
	}

	manifestMu.Lock()
	_ = saveManifest(mPath, &manifest)
	manifestMu.Unlock()
	if err := os.Remove(mPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	printStatsLine(writeStats)
	fmt.Println("Download completed successfully.")
	return nil
}

func runQueueMode(client *http.Client, rawURL string, outputPath string, connections int, maxRetries int, mPath string, manifest *DownloadManifest, manifestMu *sync.Mutex, downloaded *int64, chunkDownloaded []int64, stopSaver chan struct{}, saverDone chan struct{}, progressDone chan struct{}, progressFinished chan struct{}, bufPool *sync.Pool, writeStats *DownloadStats) error {
	taskCh := make(chan SegmentTask, connections*2)
	errChan := make(chan error, connections)
	var workerWG sync.WaitGroup

	for i := 0; i < connections; i++ {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			for task := range taskCh {
				if err := downloadSegmentWithRetry(client, rawURL, outputPath, task, downloaded, &chunkDownloaded[task.ChunkIndex], maxRetries, bufPool, writeStats); err != nil {
					errChan <- fmt.Errorf("segment %d (%d-%d) failed: %w", task.ChunkIndex, task.Start, task.End, err)
					return
				}

				manifestMu.Lock()
				c := &manifest.Chunks[task.ChunkIndex]
				c.CompletedRanges = append(c.CompletedRanges, ByteRange{Start: task.Start, End: task.End})
				normalizeChunk(c)
				saveErr := saveManifest(mPath, manifest)
				manifestMu.Unlock()
				if saveErr != nil {
					errChan <- fmt.Errorf("segment %d manifest save failed: %w", task.ChunkIndex, saveErr)
					return
				}
			}
		}()
	}

	for i := range manifest.Chunks {
		c := manifest.Chunks[i]
		if c.Done {
			continue
		}
		missing := missingRanges(c.Start, c.End, c.CompletedRanges)
		for _, r := range missing {
			taskCh <- SegmentTask{ChunkIndex: i, Start: r.Start, End: r.End}
		}
	}
	close(taskCh)

	workerWG.Wait()
	close(stopSaver)
	<-saverDone
	close(progressDone)
	<-progressFinished
	close(errChan)

	for runErr := range errChan {
		if runErr != nil {
			return runErr
		}
	}

	manifestMu.Lock()
	_ = saveManifest(mPath, manifest)
	manifestMu.Unlock()
	if err := os.Remove(mPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	printStatsLine(writeStats)
	fmt.Println("Download completed successfully.")
	return nil
}

func shouldMeasureWriteDisk(outputPath, writeDisk string) bool {
	if strings.TrimSpace(writeDisk) == "" {
		return true
	}
	return strings.EqualFold(filepath.VolumeName(outputPath), strings.TrimSpace(writeDisk))
}

func printStatsLine(writeStats *DownloadStats) {
	if writeStats == nil {
		return
	}
	fmt.Printf("STATS write_nanos=%d write_pct=%.2f\n", writeStats.WriteNanos(), writeStats.WritePercentApprox())
}
