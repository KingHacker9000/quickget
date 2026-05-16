package quickget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"quickget/pkg/quickget/core"
	"quickget/pkg/quickget/hash"
	"quickget/pkg/quickget/manifest"
	"quickget/pkg/quickget/probe"
	"quickget/pkg/quickget/progress"
	"quickget/pkg/quickget/tune"
)

type DownloadRequest struct {
	Options  core.Request
	Reporter progress.Reporter
}

type DownloadOptions struct {
	URL                string
	OutputPath         string
	Directory          string
	Connections        int
	Retries            int
	QueueMode          bool
	SegmentSize        int64
	BufferSize         int
	AutoBuffer         bool
	HTTP1              bool
	MaxIdleConns       int
	IdleTimeoutSeconds int
	Headers            map[string]string
	UserAgent          string
	Dynamic            bool
	MinSplitSize       int64
	MinDynamicFileSize int64
	WriteDisk          string
	ProgressIntervalMs int
}

type DownloadEvent struct {
	Type       string
	Downloaded int64
	Total      int64
	Percent    float64
	SpeedMBps  float64
	AvgMBps    float64
	Message    string
	Error      string
}

type EventCallback func(DownloadEvent)

var ErrDownloadCancelled = errors.New("download cancelled")

type DownloadResult = core.Result

type InspectResult = probe.URLInfo

type FileStatsResult = probe.RemoteFileStats

type ServerTestResult = probe.ServerProbeResult

type ManifestStatusResult struct {
	ManifestPath string
	URL          string
	OutputPath   string
	TotalSize    int64
	Connections  int
	QueueMode    bool
	SegmentSize  int64
	Downloaded   int64
	Total        int64
	DoneChunks   int
	Chunks       int
	Percent      float64
	State        string
	RawJSON      string
}

type ManifestCleanResult struct {
	ManifestPath       string
	ManifestRemoved    bool
	OutputRemoved      bool
	OutputPreserved    bool
	OutputMissing      bool
	ManifestWasMissing bool
}

func DownloadWithRequest(ctx context.Context, req DownloadRequest) (DownloadResult, error) {
	opts := req.Options
	opts.ProgressReporter = req.Reporter
	return core.Download(ctx, opts)
}

func Download(ctx context.Context, opts DownloadOptions, emit EventCallback) error {
	if emit != nil {
		emit(DownloadEvent{
			Type:    "start",
			Message: "download started",
		})
	}

	reporter := func(s progress.Snapshot) {
		if emit == nil {
			return
		}
		emit(DownloadEvent{
			Type:       "progress",
			Downloaded: s.Downloaded,
			Total:      s.Total,
			Percent:    s.Percent,
			SpeedMBps:  s.SpeedMBps,
			AvgMBps:    s.SpeedMBps,
		})
	}

	req := DownloadRequest{
		Options:  toCoreRequest(opts),
		Reporter: reporter,
	}
	res, err := DownloadWithRequest(ctx, req)
	if err != nil {
		if isCancellationError(err) {
			if emit != nil {
				emit(DownloadEvent{
					Type:    "cancelled",
					Message: "download cancelled; resume state saved",
					Error:   err.Error(),
				})
			}
			return fmt.Errorf("%w: %w", ErrDownloadCancelled, context.Canceled)
		}
		if emit != nil {
			emit(DownloadEvent{
				Type:  "error",
				Error: err.Error(),
			})
		}
		return err
	}

	if emit != nil {
		emit(DownloadEvent{
			Type:    "complete",
			Total:   res.Size,
			Message: res.OutputPath,
		})
	}
	return nil
}

func toCoreRequest(opts DownloadOptions) core.Request {
	req := core.DefaultRequest()
	req.URL = strings.TrimSpace(opts.URL)
	if output := strings.TrimSpace(opts.OutputPath); output != "" {
		req.OutputPath = output
	}
	if dir := strings.TrimSpace(opts.Directory); dir != "" {
		req.OutputDir = dir
	}
	if opts.Connections > 0 {
		req.Workers = opts.Connections
	}
	if opts.Retries > 0 {
		req.Retries = opts.Retries
	}
	req.QueueMode = opts.QueueMode
	if opts.SegmentSize > 0 {
		req.SegmentSize = opts.SegmentSize
	}
	if opts.BufferSize > 0 {
		req.BufferSize = opts.BufferSize
		req.BufferSizeSet = true
	}
	req.AutoBuffer = opts.AutoBuffer
	if opts.HTTP1 {
		req.ForceHTTP1 = true
	}
	if opts.MaxIdleConns > 0 {
		req.MaxIdleConns = opts.MaxIdleConns
	}
	if opts.IdleTimeoutSeconds > 0 {
		req.IdleTimeoutSec = opts.IdleTimeoutSeconds
	}
	if userAgent := strings.TrimSpace(opts.UserAgent); userAgent != "" {
		req.UserAgent = userAgent
	}
	req.Dynamic = opts.Dynamic
	if opts.MinSplitSize > 0 {
		req.MinSplitSize = opts.MinSplitSize
	}
	if opts.MinDynamicFileSize > 0 {
		req.MinDynamicFileSize = opts.MinDynamicFileSize
	}
	if writeDisk := strings.TrimSpace(opts.WriteDisk); writeDisk != "" {
		req.WriteDisk = writeDisk
	}
	req.ProgressIntervalMs = opts.ProgressIntervalMs
	req.Stdout = io.Discard
	req.Headers = make(http.Header)
	for k, v := range opts.Headers {
		if strings.TrimSpace(k) == "" {
			continue
		}
		req.Headers.Set(k, v)
	}
	return req
}

func isCancellationError(err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "download cancelled")
}

func Inspect(rawURL string, client *http.Client) (InspectResult, error) {
	if client == nil {
		client = core.NewHTTPClient(1, core.DefaultForceHTTP1, core.DefaultMaxIdleConns, core.DefaultIdleTimeoutSec)
	}
	validatedURL, err := probe.ValidateURL(rawURL)
	if err != nil {
		return InspectResult{}, err
	}
	return probe.FetchURLInfo(context.Background(), client, validatedURL, nil, core.DefaultUserAgent, core.ApplyHeaders)
}

func FileStats(rawURL string, client *http.Client) (FileStatsResult, error) {
	if client == nil {
		client = core.NewHTTPClient(1, core.DefaultForceHTTP1, core.DefaultMaxIdleConns, core.DefaultIdleTimeoutSec)
	}
	validatedURL, err := probe.ValidateURL(rawURL)
	if err != nil {
		return FileStatsResult{}, err
	}
	return probe.FetchRemoteFileStats(context.Background(), client, validatedURL, nil, core.DefaultUserAgent, core.ApplyHeaders)
}

func ServerTest(rawURL string, client *http.Client) (ServerTestResult, error) {
	if client == nil {
		client = core.NewHTTPClient(1, core.DefaultForceHTTP1, core.DefaultMaxIdleConns, core.DefaultIdleTimeoutSec)
	}
	res, err := probe.ProbeServer(context.Background(), rawURL, client, core.ApplyHeaders)
	if err != nil {
		return ServerTestResult{}, err
	}
	return *res, nil
}

func HashFile(path string) (string, error) {
	return hash.FileSHA256(path)
}

func RecommendBuffer(path string) (tune.DiskTestResult, error) {
	return tune.RecommendBufferSizeForPath(path)
}

func RunDiskTest(path string) ([]tune.DiskTestResult, tune.DiskTestResult, error) {
	return tune.RunDiskBufferTest(path)
}

func ManifestStatus(outputPath string) (ManifestStatusResult, error) {
	path := manifest.Path(outputPath)
	m, err := manifest.Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ManifestStatusResult{}, fmt.Errorf("manifest not found: %s", path)
		}
		return ManifestStatusResult{}, err
	}

	downloaded, total, doneChunks := manifest.Totals(m)
	percent := 0.0
	if total > 0 {
		percent = float64(downloaded) / float64(total) * 100
		if percent > 100 {
			percent = 100
		}
	}
	state := "incomplete"
	if doneChunks == len(m.Chunks) && len(m.Chunks) > 0 {
		state = "complete"
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return ManifestStatusResult{}, err
	}

	return ManifestStatusResult{
		ManifestPath: path,
		URL:          m.URL,
		OutputPath:   m.OutputPath,
		TotalSize:    m.TotalSize,
		Connections:  m.Connections,
		QueueMode:    m.QueueMode,
		SegmentSize:  m.SegmentSize,
		Downloaded:   downloaded,
		Total:        total,
		DoneChunks:   doneChunks,
		Chunks:       len(m.Chunks),
		Percent:      percent,
		State:        state,
		RawJSON:      string(data),
	}, nil
}

func ManifestClean(outputPath string) (ManifestCleanResult, error) {
	path := manifest.Path(outputPath)
	m, err := manifest.Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ManifestCleanResult{
				ManifestPath:       path,
				ManifestWasMissing: true,
				OutputPreserved:    true,
			}, nil
		}
		return ManifestCleanResult{}, err
	}

	complete := manifest.Complete(m)
	result := ManifestCleanResult{ManifestPath: path}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return ManifestCleanResult{}, err
	}
	result.ManifestRemoved = true

	if complete {
		result.OutputPreserved = true
		return result, nil
	}

	if err := os.Remove(outputPath); err != nil {
		if os.IsNotExist(err) {
			result.OutputMissing = true
			return result, nil
		}
		return ManifestCleanResult{}, err
	}

	result.OutputRemoved = true
	return result, nil
}
