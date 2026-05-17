package agent

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"quickget/pkg/quickget/bench"
)

const (
	profilerDefaultURL     = "https://proof.ovh.net/files/1Gb.dat"
	profilerDefaultRepeats = 2
)

var profileAllowedSizes = map[string]int64{
	"10MB":  10 * 1024 * 1024,
	"100MB": 100 * 1024 * 1024,
	"1GB":   1024 * 1024 * 1024,
}

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

type ProfilerRunRequest struct {
	Level   string `json:"level,omitempty"`
	Sizes   string `json:"sizes,omitempty"`
	Repeats int    `json:"repeats,omitempty"`
	URL     string `json:"url,omitempty"`
}

type profilerRunResult struct {
	Recommendation ProfilerRecommendation
	Artifacts      ProfilerArtifacts
}

type profilerRunner interface {
	Run(ctx context.Context, req ProfilerRunRequest, runID string, emit func(stage, msg string, data map[string]any)) (profilerRunResult, error)
}

type defaultProfilerRunner struct{}

func (defaultProfilerRunner) Run(ctx context.Context, req ProfilerRunRequest, runID string, emit func(stage, msg string, data map[string]any)) (profilerRunResult, error) {
	level := strings.ToLower(strings.TrimSpace(req.Level))
	if level == "" {
		level = "normal"
	}
	if level != "quick" && level != "normal" && level != "exhaustive" {
		return profilerRunResult{}, fmt.Errorf("invalid level %q: allowed quick, normal, exhaustive", req.Level)
	}

	repeats := req.Repeats
	if repeats <= 0 {
		repeats = profilerDefaultRepeats
	}
	sizes, err := parseSizesCSV(req.Sizes)
	if err != nil {
		return profilerRunResult{}, err
	}
	sourceURL := strings.TrimSpace(req.URL)
	if sourceURL == "" {
		sourceURL = profilerDefaultURL
	}

	configs := buildProfilerConfigs(level, sourceURL, sizes)
	if len(configs) == 0 {
		return profilerRunResult{}, fmt.Errorf("no benchmark configurations generated")
	}

	emit("prepare", fmt.Sprintf("Prepared profile run: level=%s sizes=%s repeats=%d", level, strings.Join(sizes, ","), repeats), map[string]any{
		"run_id":     runID,
		"step_index": 1,
		"step_total": len(configs)*repeats + 2,
		"level":      level,
		"sizes":      sizes,
		"repeats":    repeats,
		"url":        sourceURL,
	})
	results := make([]bench.BenchmarkResult, 0, len(configs)*repeats)
	totalRuns := len(configs) * repeats
	step := 1
	start := time.Now()
	for _, cfg := range configs {
		for repeat := 1; repeat <= repeats; repeat++ {
			select {
			case <-ctx.Done():
				return profilerRunResult{}, ctx.Err()
			default:
			}
			step++
			elapsed := time.Since(start)
			var eta time.Duration
			if step > 1 {
				avg := elapsed / time.Duration(step-1)
				eta = avg * time.Duration(totalRuns-(step-1))
			}
			emit("benchmark", fmt.Sprintf("Run %d/%d size=%s n=%d seg=%s buf=%s http=%s repeat=%d/%d ETA=%s", step-1, totalRuns, cfg.TestSizeLabel, cfg.Connections, formatBytesShort(cfg.SegmentSize), formatBytesShort(int64(cfg.BufferSize)), cfg.HTTPMode, repeat, repeats, formatETA(eta)), map[string]any{
				"run_id":     runID,
				"size_label": cfg.TestSizeLabel,
				"step_index": step - 1,
				"step_total": totalRuns + 2,
				"level":      level,
			})
			cfg.RepeatIndex = repeat
			results = append(results, bench.RunBenchmark(ctx, cfg))
		}
	}

	emit("aggregate", "Selecting best recommendation...", map[string]any{
		"run_id":     runID,
		"step_index": totalRuns + 1,
		"step_total": totalRuns + 2,
	})
	recommendation, err := recommendFromResults(results)
	if err != nil {
		return profilerRunResult{}, err
	}

	emit("persist", "Writing profiler artifacts...", map[string]any{
		"run_id":     runID,
		"step_index": totalRuns + 2,
		"step_total": totalRuns + 2,
	})
	artifacts, err := writeProfilerArtifacts(results)
	if err != nil {
		return profilerRunResult{}, err
	}
	return profilerRunResult{Recommendation: recommendation, Artifacts: artifacts}, nil
}

func buildProfilerConfigs(level, url string, sizes []string) []bench.BenchmarkConfig {
	conns := []int{1, 2, 4, 8}
	segments := []int64{4 * 1024 * 1024, 8 * 1024 * 1024, 16 * 1024 * 1024}
	buffers := []int{512 * 1024, 1024 * 1024}
	if level == "normal" {
		conns = []int{1, 2, 4, 8, 12, 16, 24, 32}
		segments = []int64{2 * 1024 * 1024, 4 * 1024 * 1024, 8 * 1024 * 1024, 16 * 1024 * 1024, 32 * 1024 * 1024}
		buffers = []int{256 * 1024, 512 * 1024, 1024 * 1024, 2 * 1024 * 1024}
	}
	if level == "exhaustive" {
		conns = []int{1, 2, 4, 8, 12, 16, 24, 32}
		segments = []int64{1 * 1024 * 1024, 2 * 1024 * 1024, 4 * 1024 * 1024, 8 * 1024 * 1024, 16 * 1024 * 1024, 32 * 1024 * 1024, 64 * 1024 * 1024}
		buffers = []int{256 * 1024, 512 * 1024, 1024 * 1024, 2 * 1024 * 1024, 4 * 1024 * 1024}
	}
	modes := []string{"auto", "http1"}
	raw := make([]bench.BenchmarkConfig, 0, len(sizes)*len(conns)*len(segments)*len(buffers)*len(modes))
	for _, sizeLabel := range sizes {
		targetBytes := profileAllowedSizes[sizeLabel]
		for _, c := range conns {
			for _, s := range segments {
				for _, b := range buffers {
					for _, m := range modes {
						raw = append(raw, bench.BenchmarkConfig{
							TestSizeLabel: sizeLabel,
							TargetBytes:   targetBytes,
							SourceURL:     url,
							Connections:   c,
							QueueMode:     true,
							SegmentSize:   s,
							BufferSize:    b,
							HTTPMode:      m,
						})
					}
				}
			}
		}
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
		SizeLabel   string
		TargetBytes int64
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
					SizeLabel:   r.TestSizeLabel,
					TargetBytes: r.TargetBytes,
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
			r.SizeLabel,
			fmt.Sprintf("%d", r.TargetBytes),
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

func parseSizesCSV(raw string) ([]string, error) {
	input := strings.TrimSpace(raw)
	if input == "" {
		input = "10MB,100MB,1GB"
	}
	parts := strings.Split(input, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		size := strings.ToUpper(strings.TrimSpace(part))
		if size == "" {
			continue
		}
		if _, ok := profileAllowedSizes[size]; !ok {
			return nil, fmt.Errorf("invalid size %q: allowed 10MB,100MB,1GB", size)
		}
		if seen[size] {
			continue
		}
		seen[size] = true
		out = append(out, size)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one size is required")
	}
	return out, nil
}

func formatBytesShort(b int64) string {
	if b%(1024*1024) == 0 {
		return fmt.Sprintf("%dMB", b/(1024*1024))
	}
	if b%1024 == 0 {
		return fmt.Sprintf("%dKB", b/1024)
	}
	return fmt.Sprintf("%dB", b)
}

func formatETA(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	sec := int64(d.Round(time.Second).Seconds())
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}
