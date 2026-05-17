package agent

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"quickget/pkg/quickget/bench"
)

const (
	profilerDefaultURL       = "https://proof.ovh.net/files/1Gb.dat"
	profilerDefaultRepeats   = 2
	profilerDefaultSizeLabel = "100MB"
	profilerDefaultSizeBytes = int64(100 * 1024 * 1024)
)

type ProfilerRecommendation struct {
	Connections int   `json:"connections"`
	QueueMode   bool  `json:"queueMode"`
	SegmentSize int64 `json:"segmentSize"`
	BufferSize  int   `json:"bufferSize"`
	HTTP1       bool  `json:"http1"`
}

type ProfilerArtifacts struct {
	ProfileDir string `json:"profileDir"`
	RawCSV     string `json:"rawCsv"`
	SummaryCSV string `json:"summaryCsv"`
}

type ProfilerState struct {
	Status         string                  `json:"status"`
	RunID          string                  `json:"runId,omitempty"`
	LastRunAt      *time.Time              `json:"lastRunAt,omitempty"`
	LastError      string                  `json:"lastError,omitempty"`
	Recommendation *ProfilerRecommendation `json:"recommendation,omitempty"`
	Artifacts      *ProfilerArtifacts      `json:"artifacts,omitempty"`
}

type profilerRunResult struct {
	Recommendation ProfilerRecommendation
	Artifacts      ProfilerArtifacts
}

type profilerRunner interface {
	Run(ctx context.Context, runID string, emit func(stage, msg string, data map[string]any)) (profilerRunResult, error)
}

type defaultProfilerRunner struct{}

func (defaultProfilerRunner) Run(ctx context.Context, runID string, emit func(stage, msg string, data map[string]any)) (profilerRunResult, error) {
	emit("prepare", "Preparing benchmark configurations...", map[string]any{
		"run_id":     runID,
		"size_label": profilerDefaultSizeLabel,
		"step_index": 1,
		"step_total": 4,
	})
	configs := buildProfilerConfigs()
	results := make([]bench.BenchmarkResult, 0, len(configs)*profilerDefaultRepeats)
	totalRuns := len(configs) * profilerDefaultRepeats
	step := 1
	for _, cfg := range configs {
		for repeat := 1; repeat <= profilerDefaultRepeats; repeat++ {
			select {
			case <-ctx.Done():
				return profilerRunResult{}, ctx.Err()
			default:
			}
			step++
			emit("benchmark", fmt.Sprintf("Running config c=%d q=%t seg=%d buf=%d http=%s (%d/%d)", cfg.Connections, cfg.QueueMode, cfg.SegmentSize, cfg.BufferSize, cfg.HTTPMode, step-1, totalRuns+2), map[string]any{
				"run_id":     runID,
				"size_label": profilerDefaultSizeLabel,
				"step_index": step - 1,
				"step_total": totalRuns + 2,
			})
			cfg.RepeatIndex = repeat
			results = append(results, bench.RunBenchmark(ctx, cfg))
		}
	}

	emit("aggregate", "Selecting best recommendation...", map[string]any{
		"run_id":     runID,
		"size_label": profilerDefaultSizeLabel,
		"step_index": totalRuns + 1,
		"step_total": totalRuns + 2,
	})
	recommendation, err := recommendFromResults(results)
	if err != nil {
		return profilerRunResult{}, err
	}

	emit("persist", "Writing profiler artifacts...", map[string]any{
		"run_id":     runID,
		"size_label": profilerDefaultSizeLabel,
		"step_index": totalRuns + 2,
		"step_total": totalRuns + 2,
	})
	artifacts, err := writeProfilerArtifacts(results)
	if err != nil {
		return profilerRunResult{}, err
	}
	return profilerRunResult{Recommendation: recommendation, Artifacts: artifacts}, nil
}

func buildProfilerConfigs() []bench.BenchmarkConfig {
	raw := []bench.BenchmarkConfig{
		{TestSizeLabel: profilerDefaultSizeLabel, TargetBytes: profilerDefaultSizeBytes, SourceURL: profilerDefaultURL, Connections: 2, QueueMode: true, SegmentSize: 8 * 1024 * 1024, BufferSize: 512 * 1024, HTTPMode: "auto"},
		{TestSizeLabel: profilerDefaultSizeLabel, TargetBytes: profilerDefaultSizeBytes, SourceURL: profilerDefaultURL, Connections: 4, QueueMode: true, SegmentSize: 8 * 1024 * 1024, BufferSize: 1024 * 1024, HTTPMode: "auto"},
		{TestSizeLabel: profilerDefaultSizeLabel, TargetBytes: profilerDefaultSizeBytes, SourceURL: profilerDefaultURL, Connections: 8, QueueMode: true, SegmentSize: 16 * 1024 * 1024, BufferSize: 1024 * 1024, HTTPMode: "auto"},
		{TestSizeLabel: profilerDefaultSizeLabel, TargetBytes: profilerDefaultSizeBytes, SourceURL: profilerDefaultURL, Connections: 8, QueueMode: true, SegmentSize: 16 * 1024 * 1024, BufferSize: 1024 * 1024, HTTPMode: "http1"},
	}
	return bench.PruneBenchmarkConfigs(raw)
}

func recommendFromResults(results []bench.BenchmarkResult) (ProfilerRecommendation, error) {
	type aggregate struct {
		connections int
		queueMode   bool
		segmentSize int64
		bufferSize  int
		httpMode    string
		sum         float64
		count       int
	}
	grouped := map[string]*aggregate{}
	for _, r := range results {
		if !r.Success {
			continue
		}
		key := fmt.Sprintf("%d|%t|%d|%d|%s", r.Connections, r.QueueMode, r.SegmentSize, r.BufferSize, r.HTTPMode)
		current, ok := grouped[key]
		if !ok {
			current = &aggregate{
				connections: r.Connections,
				queueMode:   r.QueueMode,
				segmentSize: r.SegmentSize,
				bufferSize:  r.BufferSize,
				httpMode:    r.HTTPMode,
			}
			grouped[key] = current
		}
		current.sum += r.AvgMBps
		current.count++
	}
	if len(grouped) == 0 {
		return ProfilerRecommendation{}, fmt.Errorf("no successful profiler runs")
	}

	best := (*aggregate)(nil)
	bestAvg := -1.0
	for _, g := range grouped {
		avg := g.sum / float64(g.count)
		if avg > bestAvg {
			best = g
			bestAvg = avg
		}
	}

	return ProfilerRecommendation{
		Connections: best.connections,
		QueueMode:   best.queueMode,
		SegmentSize: best.segmentSize,
		BufferSize:  best.bufferSize,
		HTTP1:       best.httpMode == "http1",
	}, nil
}

func writeProfilerArtifacts(results []bench.BenchmarkResult) (ProfilerArtifacts, error) {
	now := time.Now().UTC()
	dir := filepath.Join(".quickget", "profiles", now.Format("2006-01-02_15-04-05"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ProfilerArtifacts{}, err
	}
	rawPath := filepath.Join(dir, "raw_results.csv")
	summaryPath := filepath.Join(dir, "summary.csv")
	if err := writeRawCSV(rawPath, results); err != nil {
		return ProfilerArtifacts{}, err
	}
	if err := writeSummaryCSV(summaryPath, results); err != nil {
		return ProfilerArtifacts{}, err
	}
	return ProfilerArtifacts{
		ProfileDir: dir,
		RawCSV:     rawPath,
		SummaryCSV: summaryPath,
	}, nil
}

func writeRawCSV(path string, results []bench.BenchmarkResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"timestamp", "size_label", "target_bytes", "connections", "queue_mode", "segment_size", "buffer_size", "http_mode", "repeat", "avg_mbps", "success", "error"}); err != nil {
		return err
	}
	for _, r := range results {
		if err := w.Write([]string{
			r.Timestamp.Format(time.RFC3339),
			r.TestSizeLabel,
			fmt.Sprintf("%d", r.TargetBytes),
			fmt.Sprintf("%d", r.Connections),
			fmt.Sprintf("%t", r.QueueMode),
			fmt.Sprintf("%d", r.SegmentSize),
			fmt.Sprintf("%d", r.BufferSize),
			r.HTTPMode,
			fmt.Sprintf("%d", r.RepeatIndex),
			fmt.Sprintf("%.4f", r.AvgMBps),
			fmt.Sprintf("%t", r.Success),
			r.ErrorMessage,
		}); err != nil {
			return err
		}
	}
	return nil
}

func writeSummaryCSV(path string, results []bench.BenchmarkResult) error {
	type row struct {
		Connections int
		QueueMode   bool
		SegmentSize int64
		BufferSize  int
		HTTPMode    string
		AvgMBps     float64
		Successes   int
		Total       int
	}
	type aggregate struct {
		row
		sum float64
	}
	grouped := map[string]*aggregate{}
	for _, r := range results {
		key := fmt.Sprintf("%d|%t|%d|%d|%s", r.Connections, r.QueueMode, r.SegmentSize, r.BufferSize, r.HTTPMode)
		current, ok := grouped[key]
		if !ok {
			current = &aggregate{
				row: row{
					Connections: r.Connections,
					QueueMode:   r.QueueMode,
					SegmentSize: r.SegmentSize,
					BufferSize:  r.BufferSize,
					HTTPMode:    r.HTTPMode,
				},
			}
			grouped[key] = current
		}
		current.Total++
		if r.Success {
			current.Successes++
			current.sum += r.AvgMBps
		}
	}
	rows := make([]row, 0, len(grouped))
	for _, g := range grouped {
		if g.Successes > 0 {
			g.AvgMBps = g.sum / float64(g.Successes)
		}
		rows = append(rows, g.row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].AvgMBps == rows[j].AvgMBps {
			return rows[i].Connections < rows[j].Connections
		}
		return rows[i].AvgMBps > rows[j].AvgMBps
	})

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"rank", "size_label", "target_bytes", "connections", "queue_mode", "segment_size", "buffer_size", "http_mode", "avg_mbps", "successes", "total"}); err != nil {
		return err
	}
	for i, r := range rows {
		if err := w.Write([]string{
			fmt.Sprintf("%d", i+1),
			profilerDefaultSizeLabel,
			fmt.Sprintf("%d", profilerDefaultSizeBytes),
			fmt.Sprintf("%d", r.Connections),
			fmt.Sprintf("%t", r.QueueMode),
			fmt.Sprintf("%d", r.SegmentSize),
			fmt.Sprintf("%d", r.BufferSize),
			r.HTTPMode,
			fmt.Sprintf("%.4f", r.AvgMBps),
			fmt.Sprintf("%d", r.Successes),
			fmt.Sprintf("%d", r.Total),
		}); err != nil {
			return err
		}
	}
	return nil
}
