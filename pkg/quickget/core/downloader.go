package core

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"quickget/pkg/quickget/manifest"
	"quickget/pkg/quickget/probe"
	"quickget/pkg/quickget/progress"
	"quickget/pkg/quickget/tune"
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
	LowDiskSpaceWarningBytes       = int64(2 * 1024 * 1024 * 1024)
	jsonSaveInterval               = 5000
	DefaultUserAgent               = probe.DefaultUserAgent
)

type Request struct {
	OutputPath         string
	OutputDir          string
	Workers            int
	Retries            int
	Verbose            bool
	Dynamic            bool
	MinSplitSize       int64
	MinDynamicFileSize int64
	QueueMode          bool
	SegmentSize        int64
	BufferSize         int
	BufferSizeSet      bool
	AutoBuffer         bool
	ForceHTTP1         bool
	MaxIdleConns       int
	IdleTimeoutSec     int
	WriteDisk          string
	UserAgent          string
	Headers            http.Header
	URL                string
	Stdout             io.Writer
	ProgressReporter   progress.Reporter
}

type Result struct {
	FinalURL        string
	OutputPath      string
	Size            int64
	Mode            string
	WriteNanos      int64
	WritePercent    float64
	RangeSupported  bool
	UsedConnections int
}

func DefaultRequest() Request {
	return Request{
		Workers:            DefaultParallelConnections,
		Retries:            3,
		Dynamic:            DefaultDynamicSplitting,
		MinSplitSize:       DefaultMinSplitSizeBytes,
		MinDynamicFileSize: DefaultMinDynamicFileSizeBytes,
		SegmentSize:        DefaultSegmentSizeBytes,
		BufferSize:         DefaultBufferSizeBytes,
		ForceHTTP1:         DefaultForceHTTP1,
		MaxIdleConns:       DefaultMaxIdleConns,
		IdleTimeoutSec:     DefaultIdleTimeoutSec,
		UserAgent:          DefaultUserAgent,
		Headers:            make(http.Header),
	}
}

func ApplyHeaders(req *http.Request, headers http.Header, userAgent string) {
	for key, values := range headers {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	if req.Header.Get("User-Agent") == "" && userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
}

func NewHTTPClient(connections int, forceHTTP1 bool, maxIdleConns int, idleTimeoutSec int) *http.Client {
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

func Download(ctx context.Context, options Request) (Result, error) {
	_ = ctx
	if options.Stdout == nil {
		options.Stdout = io.Discard
	}
	validatedURL, err := probe.ValidateURL(options.URL)
	if err != nil {
		return Result{}, err
	}

	client := NewHTTPClient(options.Workers, options.ForceHTTP1, options.MaxIdleConns, options.IdleTimeoutSec)

	fmt.Fprintln(options.Stdout, "URL:", validatedURL)
	fmt.Fprintln(options.Stdout, "Parallel connections:", options.Workers)
	fmt.Fprintln(options.Stdout, "Dynamic splitting:", options.Dynamic)
	fmt.Fprintln(options.Stdout, "Queue mode:", options.QueueMode)
	if options.QueueMode {
		fmt.Fprintln(options.Stdout, "Segment size:", options.SegmentSize)
	}

	info, err := probe.FetchURLInfo(client, validatedURL, options.Headers, options.UserAgent, ApplyHeaders)
	if err != nil {
		return Result{}, fmt.Errorf("HEAD request failed: %w", err)
	}

	fmt.Fprintln(options.Stdout, "Final URL:", info.FinalURL)
	fmt.Fprintln(options.Stdout, "File size:", info.Size)
	fmt.Fprintln(options.Stdout, "Range supported:", info.RangeSupported)
	if options.OutputPath == "" {
		options.OutputPath = info.SuggestedOutputName
	}
	if options.OutputDir == "" {
		options.OutputDir = DefaultDownloadDir()
	}
	options.OutputPath = filepath.Join(options.OutputDir, options.OutputPath)
	fmt.Fprintln(options.Stdout, "Output:", options.OutputPath)
	if info.Size > 0 {
		freeSpace, err := AvailableDiskSpace(options.OutputPath)
		if err != nil {
			return Result{}, fmt.Errorf("disk space check failed: %w", err)
		}
		if freeSpace < info.Size {
			return Result{}, fmt.Errorf("insufficient disk space: need %s, available %s", tune.FormatBytesBinary(int(info.Size)), tune.FormatBytesBinary(int(freeSpace)))
		}
		if freeSpace-info.Size < LowDiskSpaceWarningBytes {
			fmt.Fprintf(options.Stdout, "warning: low remaining disk space after download: %s left\n", tune.FormatBytesBinary(int(freeSpace-info.Size)))
		}
	}

	if options.AutoBuffer && !options.BufferSizeSet {
		rec, err := tune.RecommendBufferSizeForPath(options.OutputPath)
		if err != nil {
			return Result{}, fmt.Errorf("auto buffer tune failed: %w", err)
		}
		options.BufferSize = rec.BufferSize
		fmt.Fprintf(options.Stdout, "Auto buffer selected: %s (%d bytes)\n", tune.FormatBytesBinary(rec.BufferSize), rec.BufferSize)
	}

	if options.QueueMode {
		segments := splitIntoSegments(info.Size, options.SegmentSize)
		if len(segments) == 0 {
			fmt.Fprintln(options.Stdout, "Segments: unavailable (file size unknown or invalid)")
		} else {
			fmt.Fprintf(options.Stdout, "Segments: %d jobs\n", len(segments))
		}
	} else {
		chunks := splitIntoChunks(info.Size, options.Workers)
		if len(chunks) == 0 {
			fmt.Fprintln(options.Stdout, "Chunks: unavailable (file size unknown or invalid)")
		} else {
			fmt.Fprintln(options.Stdout, "Chunks:")
			for _, c := range chunks {
				fmt.Fprintf(options.Stdout, "  [%d] %d-%d\n", c.Index, c.Start, c.End)
			}
		}
	}

	useSingle := info.Size <= 0 || !info.RangeSupported || options.Workers <= 1
	if useSingle {
		fmt.Fprintln(options.Stdout, "Mode: single download")
		if err := downloadSingle(client, info.FinalURL, options.OutputPath, info.Size, options.BufferSize, options.Headers, options.UserAgent, options.Stdout, options.ProgressReporter); err != nil {
			return Result{}, fmt.Errorf("download failed: %w", err)
		}
		return Result{
			FinalURL:        info.FinalURL,
			OutputPath:      options.OutputPath,
			Size:            info.Size,
			Mode:            "single",
			RangeSupported:  info.RangeSupported,
			UsedConnections: 1,
		}, nil
	}

	fmt.Fprintln(options.Stdout, "Mode: parallel download")
	writeStats, err := downloadParallel(client, info.FinalURL, options.OutputPath, info.Size, options.Workers, options.Verbose, options.Retries, options.Dynamic, options.MinSplitSize, options.MinDynamicFileSize, options.QueueMode, options.SegmentSize, options.BufferSize, options.WriteDisk, options.Headers, options.UserAgent, options.Stdout, options.ProgressReporter)
	if err != nil {
		return Result{}, fmt.Errorf("parallel download failed: %w", err)
	}

	res := Result{
		FinalURL:        info.FinalURL,
		OutputPath:      options.OutputPath,
		Size:            info.Size,
		Mode:            "parallel",
		RangeSupported:  info.RangeSupported,
		UsedConnections: options.Workers,
	}
	if writeStats != nil {
		res.WriteNanos = writeStats.WriteNanos()
		res.WritePercent = writeStats.WritePercentApprox()
	}
	return res, nil
}

func splitIntoChunks(size int64, connections int) []manifest.Chunk {
	if size <= 0 || connections <= 0 {
		return nil
	}
	if int64(connections) > size {
		connections = int(size)
	}

	chunks := make([]manifest.Chunk, 0, connections)
	baseSize := size / int64(connections)
	remainder := size % int64(connections)

	start := int64(0)
	for i := 0; i < connections; i++ {
		partSize := baseSize
		if int64(i) < remainder {
			partSize++
		}

		end := start + partSize - 1
		chunks = append(chunks, manifest.Chunk{Index: i, Start: start, End: end})
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

func downloadSegment(client *http.Client, rawURL string, outputPath string, task manifest.SegmentTask, downloaded *int64, chunkDownloaded *int64, bufPool *sync.Pool, stats *progress.DownloadStats, headers http.Header, userAgent string) error {
	if task.End < task.Start {
		return fmt.Errorf("invalid range %d-%d", task.Start, task.End)
	}

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	ApplyHeaders(req, headers, userAgent)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", task.Start, task.End))

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	byteRange := fmt.Sprintf("bytes=%d-%d", task.Start, task.End)
	if err := probe.ExplainServerStatus(resp.StatusCode, true, byteRange, rawURL); err != nil {
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

func downloadSegmentWithRetry(client *http.Client, rawURL string, outputPath string, task manifest.SegmentTask, downloaded *int64, chunkDownloaded *int64, maxRetries int, bufPool *sync.Pool, stats *progress.DownloadStats, headers http.Header, userAgent string) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second)
		}
		err := downloadSegment(client, rawURL, outputPath, task, downloaded, chunkDownloaded, bufPool, stats, headers, userAgent)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}

func downloadSingle(client *http.Client, rawURL string, outputPath string, totalSize int64, bufferSize int, headers http.Header, userAgent string, out io.Writer, reporter progress.Reporter) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	ApplyHeaders(req, headers, userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET failed with status: %s", resp.Status)
	}

	outFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	var downloaded int64
	progressDone, progressFinished := progress.StartProgressLoop(out, &downloaded, totalSize, reporter, nil)
	defer func() {
		close(progressDone)
		<-progressFinished
	}()

	buf := make([]byte, bufferSize)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			written, writeErr := outFile.Write(buf[:n])
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

func splitIntoSegments(size int64, segmentSize int64) []manifest.Chunk {
	if size <= 0 || segmentSize <= 0 {
		return nil
	}
	chunks := make([]manifest.Chunk, 0, (size+segmentSize-1)/segmentSize)
	var index int
	for start := int64(0); start < size; start += segmentSize {
		end := start + segmentSize - 1
		if end >= size {
			end = size - 1
		}
		chunks = append(chunks, manifest.Chunk{Index: index, Start: start, End: end})
		index++
	}
	return chunks
}

func downloadParallel(client *http.Client, rawURL string, outputPath string, totalSize int64, connections int, verbose bool, maxRetries int, dynamic bool, minSplitSize int64, minDynamicFileSize int64, queueMode bool, segmentSize int64, bufferSize int, writeDisk string, headers http.Header, userAgent string, out io.Writer, reporter progress.Reporter) (*progress.DownloadStats, error) {
	mPath := manifest.Path(outputPath)
	var m manifest.DownloadManifest

	existing, err := manifest.Load(mPath)
	if err == nil &&
		existing.URL == rawURL &&
		existing.OutputPath == outputPath &&
		existing.TotalSize == totalSize &&
		existing.Connections == connections &&
		existing.QueueMode == queueMode &&
		(!queueMode || existing.SegmentSize == segmentSize) {
		m = *existing
	} else {
		var chunks []manifest.Chunk
		if queueMode {
			chunks = splitIntoSegments(totalSize, segmentSize)
		} else {
			chunks = splitIntoChunks(totalSize, connections)
		}
		if len(chunks) == 0 {
			return nil, errors.New("cannot split file into chunks")
		}
		m = manifest.DownloadManifest{
			URL:         rawURL,
			OutputPath:  outputPath,
			TotalSize:   totalSize,
			Connections: connections,
			QueueMode:   queueMode,
			SegmentSize: segmentSize,
			Chunks:      chunks,
		}
		if err := preallocateFile(outputPath, totalSize); err != nil {
			return nil, err
		}
		if err := manifest.Save(mPath, &m); err != nil {
			return nil, err
		}
	}

	if dynamic && totalSize < minDynamicFileSize {
		fmt.Fprintf(out, "Dynamic splitting disabled for this file size (size=%d, min=%d).\n", totalSize, minDynamicFileSize)
		dynamic = false
	}

	for i := range m.Chunks {
		manifest.NormalizeChunk(&m.Chunks[i])
	}

	var manifestMu sync.Mutex
	bufPool := &sync.Pool{
		New: func() interface{} {
			return make([]byte, bufferSize)
		},
	}
	remaining := make(map[int][]manifest.ByteRange)
	for i, c := range m.Chunks {
		if c.Done {
			continue
		}
		missing := manifest.MissingRanges(c.Start, c.End, c.CompletedRanges)
		if len(missing) > 0 {
			remaining[i] = missing
		}
	}

	var downloaded int64
	var writeStats *progress.DownloadStats
	measureWrites := shouldMeasureWriteDisk(outputPath, writeDisk)
	if measureWrites {
		writeStats = progress.NewDownloadStats()
	}
	chunkDownloaded := make([]int64, len(m.Chunks))
	chunkTotals := make([]int64, len(m.Chunks))
	for i, c := range m.Chunks {
		chunkDownloaded[i] = c.DownloadedBytes
		chunkTotals[i] = manifest.ChunkSize(c)
		atomic.AddInt64(&downloaded, c.DownloadedBytes)
	}

	var progressDone chan struct{}
	var progressFinished chan struct{}
	if verbose {
		progressDone, progressFinished = progress.StartVerboseProgressLoop(out, &downloaded, totalSize, chunkTotals, chunkDownloaded, reporter, writeStats)
	} else {
		progressDone, progressFinished = progress.StartProgressLoop(out, &downloaded, totalSize, reporter, writeStats)
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
				err := manifest.Save(mPath, &m)
				manifestMu.Unlock()
				if err != nil {
					fmt.Fprintf(out, "\nwarning: manifest save failed: %v\n", err)
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
		_ = manifest.Save(mPath, &m)
		manifestMu.Unlock()
		os.Exit(130)
	}()

	if queueMode {
		return runQueueMode(client, rawURL, outputPath, connections, maxRetries, mPath, &m, &manifestMu, &downloaded, chunkDownloaded, stopSaver, saverDone, progressDone, progressFinished, bufPool, writeStats, headers, userAgent, out)
	}

	pickTask := func() (manifest.SegmentTask, bool) {
		manifestMu.Lock()
		defer manifestMu.Unlock()

		maxChunk := -1
		maxRangeIdx := -1
		maxLen := int64(0)
		var maxRange manifest.ByteRange

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
			return manifest.SegmentTask{}, false
		}

		assign := maxRange
		if dynamic && maxLen >= minSplitSize*2 {
			assignSize := maxLen / 2
			assign = manifest.ByteRange{Start: maxRange.Start, End: maxRange.Start + assignSize - 1}
			remaining[maxChunk][maxRangeIdx].Start = assign.End + 1
		} else {
			last := len(remaining[maxChunk]) - 1
			remaining[maxChunk][maxRangeIdx] = remaining[maxChunk][last]
			remaining[maxChunk] = remaining[maxChunk][:last]
			if len(remaining[maxChunk]) == 0 {
				delete(remaining, maxChunk)
			}
		}

		return manifest.SegmentTask{ChunkIndex: maxChunk, Start: assign.Start, End: assign.End}, true
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

				if err := downloadSegmentWithRetry(client, rawURL, outputPath, task, &downloaded, &chunkDownloaded[task.ChunkIndex], maxRetries, bufPool, writeStats, headers, userAgent); err != nil {
					errChan <- fmt.Errorf("chunk %d segment %d-%d failed: %w", task.ChunkIndex, task.Start, task.End, err)
					return
				}

				manifestMu.Lock()
				c := &m.Chunks[task.ChunkIndex]
				c.CompletedRanges = append(c.CompletedRanges, manifest.ByteRange{Start: task.Start, End: task.End})
				manifest.NormalizeChunk(c)
				saveErr := manifest.Save(mPath, &m)
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
			return nil, runErr
		}
	}

	manifestMu.Lock()
	_ = manifest.Save(mPath, &m)
	manifestMu.Unlock()
	if err := os.Remove(mPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	printStatsLine(out, writeStats)
	fmt.Fprintln(out, "Download completed successfully.")
	return writeStats, nil
}

func runQueueMode(client *http.Client, rawURL string, outputPath string, connections int, maxRetries int, mPath string, m *manifest.DownloadManifest, manifestMu *sync.Mutex, downloaded *int64, chunkDownloaded []int64, stopSaver chan struct{}, saverDone chan struct{}, progressDone chan struct{}, progressFinished chan struct{}, bufPool *sync.Pool, writeStats *progress.DownloadStats, headers http.Header, userAgent string, out io.Writer) (*progress.DownloadStats, error) {
	taskCh := make(chan manifest.SegmentTask, connections*2)
	errChan := make(chan error, connections)
	var workerWG sync.WaitGroup

	for i := 0; i < connections; i++ {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			for task := range taskCh {
				if err := downloadSegmentWithRetry(client, rawURL, outputPath, task, downloaded, &chunkDownloaded[task.ChunkIndex], maxRetries, bufPool, writeStats, headers, userAgent); err != nil {
					errChan <- fmt.Errorf("segment %d (%d-%d) failed: %w", task.ChunkIndex, task.Start, task.End, err)
					return
				}

				manifestMu.Lock()
				c := &m.Chunks[task.ChunkIndex]
				c.CompletedRanges = append(c.CompletedRanges, manifest.ByteRange{Start: task.Start, End: task.End})
				manifest.NormalizeChunk(c)
				saveErr := manifest.Save(mPath, m)
				manifestMu.Unlock()
				if saveErr != nil {
					errChan <- fmt.Errorf("segment %d manifest save failed: %w", task.ChunkIndex, saveErr)
					return
				}
			}
		}()
	}

	for i := range m.Chunks {
		c := m.Chunks[i]
		if c.Done {
			continue
		}
		missing := manifest.MissingRanges(c.Start, c.End, c.CompletedRanges)
		for _, r := range missing {
			taskCh <- manifest.SegmentTask{ChunkIndex: i, Start: r.Start, End: r.End}
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
			return nil, runErr
		}
	}

	manifestMu.Lock()
	_ = manifest.Save(mPath, m)
	manifestMu.Unlock()
	if err := os.Remove(mPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	printStatsLine(out, writeStats)
	fmt.Fprintln(out, "Download completed successfully.")
	return writeStats, nil
}

func shouldMeasureWriteDisk(outputPath, writeDisk string) bool {
	if strings.TrimSpace(writeDisk) == "" {
		return true
	}
	return strings.EqualFold(filepath.VolumeName(outputPath), strings.TrimSpace(writeDisk))
}

func printStatsLine(out io.Writer, writeStats *progress.DownloadStats) {
	if writeStats == nil {
		return
	}
	fmt.Fprintf(out, "STATS write_nanos=%d write_pct=%.2f\n", writeStats.WriteNanos(), writeStats.WritePercentApprox())
}

func DefaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, "Downloads")
	}
	return "."
}

func AvailableDiskSpace(targetPath string) (int64, error) {
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return 0, err
	}

	checkPath := filepath.Dir(absPath)
	if runtime.GOOS == "windows" {
		volume := filepath.VolumeName(absPath)
		if volume == "" {
			return 0, errors.New("unable to determine target volume")
		}
		cmd := exec.Command("powershell", "-NoProfile", "-Command", fmt.Sprintf("[int64](Get-CimInstance Win32_LogicalDisk -Filter \"DeviceID='%s'\").FreeSpace", volume))
		out, err := cmd.Output()
		if err != nil {
			return 0, err
		}
		free, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
		if err != nil {
			return 0, err
		}
		return free, nil
	}

	cmd := exec.Command("df", "-Pk", checkPath)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, errors.New("unexpected df output")
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, errors.New("unexpected df output fields")
	}
	availableKB, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return 0, err
	}
	return availableKB * 1024, nil
}
