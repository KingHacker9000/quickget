package quickget

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

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

func Download(ctx context.Context, req DownloadRequest) (DownloadResult, error) {
	opts := req.Options
	opts.ProgressReporter = req.Reporter
	return core.Download(ctx, opts)
}

func Inspect(rawURL string, client *http.Client) (InspectResult, error) {
	if client == nil {
		client = core.NewHTTPClient(1, core.DefaultForceHTTP1, core.DefaultMaxIdleConns, core.DefaultIdleTimeoutSec)
	}
	validatedURL, err := probe.ValidateURL(rawURL)
	if err != nil {
		return InspectResult{}, err
	}
	return probe.FetchURLInfo(client, validatedURL, nil, core.DefaultUserAgent, core.ApplyHeaders)
}

func FileStats(rawURL string, client *http.Client) (FileStatsResult, error) {
	if client == nil {
		client = core.NewHTTPClient(1, core.DefaultForceHTTP1, core.DefaultMaxIdleConns, core.DefaultIdleTimeoutSec)
	}
	validatedURL, err := probe.ValidateURL(rawURL)
	if err != nil {
		return FileStatsResult{}, err
	}
	return probe.FetchRemoteFileStats(client, validatedURL, nil, core.DefaultUserAgent, core.ApplyHeaders)
}

func ServerTest(rawURL string, client *http.Client) (ServerTestResult, error) {
	if client == nil {
		client = core.NewHTTPClient(1, core.DefaultForceHTTP1, core.DefaultMaxIdleConns, core.DefaultIdleTimeoutSec)
	}
	res, err := probe.ProbeServer(rawURL, client, core.ApplyHeaders)
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
