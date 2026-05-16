package bench

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"quickget/pkg/quickget/core"
	"quickget/pkg/quickget/manifest"
	"quickget/pkg/quickget/progress"
)

type BenchmarkConfig struct {
	TestSizeLabel  string
	TargetBytes    int64
	SourceURL      string
	Connections    int
	QueueMode      bool
	SegmentSize    int64
	BufferSize     int
	HTTPMode       string
	RepeatIndex    int
	OutputTempPath string
}

type BenchmarkResult struct {
	Timestamp            time.Time
	QuickGetVersion      string
	OS                   string
	Arch                 string
	CPUCount             int
	GoVersion            string
	TestSizeLabel        string
	TargetBytes          int64
	SourceURL            string
	Connections          int
	QueueMode            bool
	SegmentSize          int64
	BufferSize           int
	HTTPMode             string
	RepeatIndex          int
	ElapsedSeconds       float64
	AvgMBps              float64
	PeakMBps             *float64
	BytesDownloaded      int64
	Success              bool
	ErrorMessage         string
	DiskWritePercent     *float64
	ServerRangeSupported *bool
}

type BenchmarkSummary struct {
	RunCount       int
	SuccessCount   int
	FailureCount   int
	AvgOfAvgMBps   float64
	MaxPeakMBps    *float64
	MeanElapsedSec float64
	TotalBytes     int64
}

type ProfileRecommendation struct {
	RecommendedConnections int
	RecommendedQueueMode   bool
	RecommendedSegmentSize int64
	RecommendedBufferSize  int
	RecommendedHTTPMode    string
	Reason                 string
}

type BenchmarkPruneStats struct {
	Generated int
	Pruned    int
	Final     int
}

func PruneBenchmarkConfigs(configs []BenchmarkConfig) []BenchmarkConfig {
	pruned, _ := PruneBenchmarkConfigsWithStats(configs)
	return pruned
}

func PruneBenchmarkConfigsWithStats(configs []BenchmarkConfig) ([]BenchmarkConfig, BenchmarkPruneStats) {
	stats := BenchmarkPruneStats{Generated: len(configs)}
	seen := make(map[string]struct{}, len(configs))
	out := make([]BenchmarkConfig, 0, len(configs))

	for _, cfg := range configs {
		if cfg.SegmentSize <= 0 {
			continue
		}
		if cfg.BufferSize <= 0 {
			continue
		}
		if cfg.Connections <= 0 {
			continue
		}
		if cfg.TargetBytes <= 0 {
			continue
		}
		if cfg.SegmentSize > cfg.TargetBytes {
			continue
		}
		if int64(cfg.BufferSize) > cfg.SegmentSize {
			continue
		}

		numSegments := (cfg.TargetBytes + cfg.SegmentSize - 1) / cfg.SegmentSize
		if cfg.Connections != 1 && numSegments < int64(cfg.Connections) {
			continue
		}

		key := benchmarkConfigDedupKey(cfg)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, cfg)
	}

	stats.Final = len(out)
	stats.Pruned = stats.Generated - stats.Final
	return out, stats
}

func benchmarkConfigDedupKey(cfg BenchmarkConfig) string {
	return fmt.Sprintf("%d|%d|%d|%d|%s",
		cfg.TargetBytes,
		cfg.Connections,
		cfg.SegmentSize,
		cfg.BufferSize,
		strings.ToLower(strings.TrimSpace(cfg.HTTPMode)),
	)
}

func RunBenchmark(ctx context.Context, cfg BenchmarkConfig) BenchmarkResult {
	result := BenchmarkResult{
		Timestamp:       time.Now().UTC(),
		QuickGetVersion: quickgetVersion(),
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		CPUCount:        runtime.NumCPU(),
		GoVersion:       runtime.Version(),
		TestSizeLabel:   cfg.TestSizeLabel,
		TargetBytes:     cfg.TargetBytes,
		SourceURL:       cfg.SourceURL,
		Connections:     cfg.Connections,
		QueueMode:       cfg.QueueMode,
		SegmentSize:     cfg.SegmentSize,
		BufferSize:      cfg.BufferSize,
		HTTPMode:        strings.ToLower(strings.TrimSpace(cfg.HTTPMode)),
		RepeatIndex:     cfg.RepeatIndex,
	}

	if result.HTTPMode == "" {
		result.HTTPMode = "auto"
	}

	outputPath, cleanupPath, err := resolveOutputPath(cfg.OutputTempPath)
	if err != nil {
		result.ErrorMessage = err.Error()
		return result
	}
	defer cleanupTempFiles(cleanupPath)

	opts := core.DefaultRequest()
	opts.URL = cfg.SourceURL
	opts.OutputPath = filepath.Base(outputPath)
	opts.OutputDir = filepath.Dir(outputPath)
	opts.Workers = cfg.Connections
	opts.QueueMode = cfg.QueueMode
	opts.SegmentSize = cfg.SegmentSize
	opts.BufferSize = cfg.BufferSize
	opts.BufferSizeSet = true
	opts.Stdout = io.Discard
	opts.ForceHTTP1 = result.HTTPMode == "http1"

	var peak float64
	reporter := func(s progress.Snapshot) {
		if s.SpeedMBps > peak {
			peak = s.SpeedMBps
		}
	}
	opts.ProgressReporter = reporter
	start := time.Now()
	downloadRes, runErr := core.DownloadSample(ctx, opts, cfg.TargetBytes)
	elapsedSec := time.Since(start).Seconds()
	result.ElapsedSeconds = elapsedSec
	if elapsedSec > 0 {
		result.AvgMBps = float64(downloadRes.Size) / 1024.0 / 1024.0 / elapsedSec
	}
	result.BytesDownloaded = downloadRes.Size

	if peak > 0 {
		peakCopy := peak
		result.PeakMBps = &peakCopy
	}
	if downloadRes.WritePercent > 0 {
		writeCopy := downloadRes.WritePercent
		result.DiskWritePercent = &writeCopy
	}
	rangeSupported := downloadRes.RangeSupported
	result.ServerRangeSupported = &rangeSupported

	if runErr != nil {
		result.ErrorMessage = runErr.Error()
		return result
	}

	result.Success = true
	return result
}

func quickgetVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return ""
	}
	if info.Main.Version == "" || info.Main.Version == "(devel)" {
		return ""
	}
	return info.Main.Version
}

func resolveOutputPath(configuredPath string) (string, string, error) {
	if strings.TrimSpace(configuredPath) != "" {
		abs, err := filepath.Abs(configuredPath)
		if err != nil {
			return "", "", err
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			return "", "", err
		}
		return abs, abs, nil
	}
	f, err := os.CreateTemp("", "quickget-bench-*.bin")
	if err != nil {
		return "", "", err
	}
	path := f.Name()
	_ = f.Close()
	return path, path, nil
}

func cleanupTempFiles(outputPath string) {
	if strings.TrimSpace(outputPath) == "" {
		return
	}
	_ = os.Remove(outputPath)
	_ = os.Remove(manifest.Path(outputPath))
}
