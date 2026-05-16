package cli

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"quickget/pkg/quickget"
	"quickget/pkg/quickget/bench"
)

const (
	profileDefaultSizes   = "10MB,100MB,1GB"
	profileDefaultRepeats = 3
	profileDefaultLevel   = "normal"
)

var (
	profileAllowedSizes = map[string]int64{
		"10MB":  10 * 1024 * 1024,
		"100MB": 100 * 1024 * 1024,
		"1GB":   1024 * 1024 * 1024,
	}
	profileBuiltInCandidateURLs = []string{
		"https://proof.ovh.net/files/1Gb.dat",
		"https://speed.hetzner.de/1GB.bin",
		"https://download.thinkbroadband.com/1GB.zip",
	}
	defaultBufferCandidates = []int{256 * 1024, 512 * 1024, 1024 * 1024, 2 * 1024 * 1024, 4 * 1024 * 1024}
)

type profileLevelConfig struct {
	ConnCandidates []int
	TopConnCount   int
	TopSegmentKeep int
	TopBufferKeep  int
	FinalKeep      int
}

type profileTopConfig struct {
	cfg     bench.BenchmarkConfig
	summary benchStats
}

type benchStats struct {
	MedianMBps  float64
	AvgMBps     float64
	StddevMBps  float64
	SuccessRate float64
	Successes   int
	Total       int
}

type profileRecommendationEntry struct {
	SizeLabel   string  `json:"size_label"`
	TargetBytes int64   `json:"target_bytes"`
	Connections int     `json:"connections"`
	QueueMode   bool    `json:"queue_mode"`
	SegmentSize int64   `json:"segment_size"`
	BufferSize  int     `json:"buffer_size"`
	HTTPMode    string  `json:"http_mode"`
	MedianMBps  float64 `json:"median_mbps"`
	AvgMBps     float64 `json:"avg_mbps"`
	StddevMBps  float64 `json:"stddev_mbps"`
	SuccessRate float64 `json:"success_rate"`
	Successes   int     `json:"successes"`
	TotalRuns   int     `json:"total_runs"`
}

type profileSizeRecommendation struct {
	BestOverall  *profileRecommendationEntry `json:"best_overall,omitempty"`
	BestHTTPAuto *profileRecommendationEntry `json:"best_http_auto,omitempty"`
	BestHTTP1    *profileRecommendationEntry `json:"best_http1,omitempty"`
	TestedAt     string                      `json:"tested_at"`
	Repeats      int                         `json:"repeats"`
	Notes        string                      `json:"notes"`
}

type profileRecommendations struct {
	SourceURL string                               `json:"source_url"`
	Sizes     map[string]profileSizeRecommendation `json:"sizes"`
}

type profileRunCollector struct {
	results []bench.BenchmarkResult
}

func (c *profileRunCollector) run(cfg bench.BenchmarkConfig) bench.BenchmarkResult {
	r := bench.RunBenchmark(context.Background(), cfg)
	c.results = append(c.results, r)
	return r
}

func (c *profileRunCollector) runMany(configs []bench.BenchmarkConfig) []bench.BenchmarkResult {
	out := make([]bench.BenchmarkResult, 0, len(configs))
	for _, cfg := range configs {
		out = append(out, c.run(cfg))
	}
	return out
}

func runProfileCommand(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		printProfileUsage(stderr, binName)
		return nil
	}

	fs := flag.NewFlagSet("profile", flag.ContinueOnError)
	fs.SetOutput(stderr)
	sizesRaw := fs.String("sizes", profileDefaultSizes, "comma-separated test sizes")
	customURL := fs.String("url", "", "custom test URL")
	repeats := fs.Int("repeats", profileDefaultRepeats, "repeat count per size")
	level := fs.String("level", profileDefaultLevel, "profile level: quick|normal|exhaustive")
	fs.Usage = func() { printProfileUsage(stderr, binName) }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("profile does not accept positional arguments")
	}
	if *repeats <= 0 {
		return errors.New("--repeats must be > 0")
	}

	normalizedLevel := strings.ToLower(strings.TrimSpace(*level))
	levelCfg, err := profileLevelParams(normalizedLevel)
	if err != nil {
		return err
	}

	sizes, err := parseProfileSizes(*sizesRaw)
	if err != nil {
		return err
	}

	maxBytes := int64(0)
	for _, s := range sizes {
		if b := profileAllowedSizes[s]; b > maxBytes {
			maxBytes = b
		}
	}
	selectedURL, probeRes, err := selectProfileURL(strings.TrimSpace(*customURL), maxBytes)
	if err != nil {
		return err
	}
	if probeRes.ContentLength > 0 && probeRes.ContentLength < maxBytes {
		return fmt.Errorf("selected URL is too small for requested size %s: file size=%d bytes", formatBytesLabel(maxBytes), probeRes.ContentLength)
	}
	if !probeRes.SupportsRange {
		return fmt.Errorf("selected URL does not support byte-range requests: %s", selectedURL)
	}

	fmt.Fprintln(stdout, "QuickGet profile (queue-mode staged tournament)")
	fmt.Fprintf(stdout, "Level: %s\n", normalizedLevel)
	fmt.Fprintf(stdout, "Repeats: %d\n", *repeats)
	fmt.Fprintf(stdout, "Source URL: %s\n", selectedURL)
	if probeRes.ContentLength >= 0 {
		fmt.Fprintf(stdout, "Remote file size: %d bytes\n", probeRes.ContentLength)
	} else {
		fmt.Fprintln(stdout, "Remote file size: unknown")
	}
	fmt.Fprintf(stdout, "Range support: %t\n", probeRes.SupportsRange)
	fmt.Fprintf(stdout, "HTTP status: %d\n", probeRes.StatusCode)
	for _, w := range probeRes.Warnings {
		fmt.Fprintf(stdout, "Warning: %s\n", w)
	}
	fmt.Fprintln(stdout)

	collector := &profileRunCollector{}
	if normalizedLevel == "exhaustive" {
		if err := runProfileExhaustive(stdout, selectedURL, sizes, *repeats, collector); err != nil {
			return err
		}
	} else {
		for _, sizeLabel := range sizes {
			targetBytes := profileAllowedSizes[sizeLabel]
			if probeRes.ContentLength > 0 && targetBytes > probeRes.ContentLength {
				return fmt.Errorf("selected URL too small for size %s: target=%d bytes file=%d bytes", sizeLabel, targetBytes, probeRes.ContentLength)
			}
			if err := runProfileForSize(stdout, selectedURL, sizeLabel, targetBytes, *repeats, levelCfg, collector); err != nil {
				return err
			}
		}
	}

	outDir, err := createProfileOutputDir(time.Now())
	if err != nil {
		return err
	}
	return writeProfileArtifacts(stdout, outDir, selectedURL, *repeats, sizes, collector.results)
}

func runProfileExhaustive(out io.Writer, url string, sizes []string, repeats int, collector *profileRunCollector) error {
	bufferCandidates := chooseBufferCandidates(url)
	if len(bufferCandidates) == 0 {
		bufferCandidates = append([]int(nil), defaultBufferCandidates...)
	}
	bufferCandidates = dedupePositiveInts(bufferCandidates)
	sort.Ints(bufferCandidates)

	baseGenerated := generateExhaustiveBaseConfigs(url, sizes, bufferCandidates)
	prunedBase, pruneStats := bench.PruneBenchmarkConfigsWithStats(baseGenerated)
	generatedRuns := len(baseGenerated) * repeats
	willRun := len(prunedBase) * repeats
	prunedRuns := generatedRuns - willRun
	fmt.Fprintf(out, "Exhaustive matrix: generated=%d pruned=%d will_run=%d (base generated=%d pruned=%d final=%d)\n",
		generatedRuns, prunedRuns, willRun, pruneStats.Generated, pruneStats.Pruned, pruneStats.Final)
	if willRun == 0 {
		return errors.New("no runnable exhaustive configurations after pruning")
	}

	runConfigs := make([]bench.BenchmarkConfig, 0, willRun)
	for _, cfg := range prunedBase {
		for r := 1; r <= repeats; r++ {
			x := cfg
			x.RepeatIndex = r
			runConfigs = append(runConfigs, x)
		}
	}

	results := make([]bench.BenchmarkResult, 0, len(runConfigs))
	start := time.Now()
	var completed int
	for _, cfg := range runConfigs {
		res := collector.run(cfg)
		results = append(results, res)
		completed++

		elapsed := time.Since(start)
		var eta time.Duration
		if completed > 0 {
			avg := elapsed / time.Duration(completed)
			eta = avg * time.Duration(len(runConfigs)-completed)
		}
		fmt.Fprintf(out, "Run %d/%d | size=%s n=%d segment=%s buffer=%s http=%s repeat=%d/%d | ETA %s\n",
			completed, len(runConfigs), cfg.TestSizeLabel, cfg.Connections, formatBytesShort(cfg.SegmentSize),
			formatBytesShort(int64(cfg.BufferSize)), cfg.HTTPMode, cfg.RepeatIndex, repeats, formatETA(eta))
	}

	rankedAll := rankAggregates(aggregateByConfig(results))
	if len(rankedAll) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Top results:")
		limit := 10
		if len(rankedAll) < limit {
			limit = len(rankedAll)
		}
		for i := 0; i < limit; i++ {
			printTopConfigSummary(out, fmt.Sprintf("#%d", i+1), rankedAll[i], repeats)
		}
	}
	return nil
}

func generateExhaustiveBaseConfigs(url string, sizes []string, bufferCandidates []int) []bench.BenchmarkConfig {
	connections := []int{1, 2, 4, 8, 12, 16, 24, 32}
	segments := []int64{1 * 1024 * 1024, 2 * 1024 * 1024, 4 * 1024 * 1024, 8 * 1024 * 1024, 16 * 1024 * 1024, 32 * 1024 * 1024, 64 * 1024 * 1024}
	modes := []string{"auto", "http1"}
	out := make([]bench.BenchmarkConfig, 0, len(sizes)*len(connections)*len(segments)*len(bufferCandidates)*len(modes))
	for _, sizeLabel := range sizes {
		targetBytes := profileAllowedSizes[sizeLabel]
		for _, n := range connections {
			for _, seg := range segments {
				for _, buf := range bufferCandidates {
					for _, mode := range modes {
						out = append(out, bench.BenchmarkConfig{TestSizeLabel: sizeLabel, TargetBytes: targetBytes, SourceURL: url, Connections: n, QueueMode: true, SegmentSize: seg, BufferSize: buf, HTTPMode: mode})
					}
				}
			}
		}
	}
	return out
}

func runProfileForSize(out io.Writer, url, sizeLabel string, targetBytes int64, repeats int, levelCfg profileLevelConfig, collector *profileRunCollector) error {
	fmt.Fprintf(out, "=== Size %s (%d bytes) ===\n", sizeLabel, targetBytes)

	segmentDefault := defaultSegmentSizeForLabel(sizeLabel)
	bufferCandidates := chooseBufferCandidates(url)
	if len(bufferCandidates) == 0 {
		bufferCandidates = append([]int(nil), defaultBufferCandidates...)
	}
	bufferCandidates = dedupePositiveInts(bufferCandidates)
	sort.Ints(bufferCandidates)
	defaultBuffer := bufferCandidates[0]
	if len(bufferCandidates) >= 3 {
		defaultBuffer = bufferCandidates[2]
	}

	fmt.Fprintf(out, "Stage1 buffers: %s\n", formatBufferList(bufferCandidates))

	workingModes := make([]string, 0, 2)
	modeFailures := make(map[string]string)
	for _, mode := range []string{"auto", "http1"} {
		cfg := bench.BenchmarkConfig{TestSizeLabel: sizeLabel, TargetBytes: targetBytes, SourceURL: url, Connections: 1, QueueMode: true, SegmentSize: segmentDefault, BufferSize: defaultBuffer, HTTPMode: mode, RepeatIndex: 1}
		res := collector.run(cfg)
		if res.Success {
			workingModes = append(workingModes, mode)
		} else {
			modeFailures[mode] = res.ErrorMessage
		}
	}
	if len(workingModes) == 0 {
		return fmt.Errorf("both HTTP modes failed pretest for %s: auto=%q http1=%q", sizeLabel, modeFailures["auto"], modeFailures["http1"])
	}
	fmt.Fprintf(out, "Stage2 HTTP modes: working=%v", workingModes)
	if len(modeFailures) > 0 {
		if msg, ok := modeFailures["auto"]; ok {
			fmt.Fprintf(out, " auto_failed=%q", msg)
		}
		if msg, ok := modeFailures["http1"]; ok {
			fmt.Fprintf(out, " http1_failed=%q", msg)
		}
	}
	fmt.Fprintln(out)

	stage3 := make([]bench.BenchmarkConfig, 0, len(workingModes)*len(levelCfg.ConnCandidates))
	for _, mode := range workingModes {
		for _, n := range levelCfg.ConnCandidates {
			stage3 = append(stage3, bench.BenchmarkConfig{TestSizeLabel: sizeLabel, TargetBytes: targetBytes, SourceURL: url, Connections: n, QueueMode: true, SegmentSize: segmentDefault, BufferSize: defaultBuffer, HTTPMode: mode, RepeatIndex: 1})
		}
	}
	stage3, stats3 := bench.PruneBenchmarkConfigsWithStats(stage3)
	fmt.Fprintf(out, "Stage3 connections: generated=%d pruned=%d final=%d\n", stats3.Generated, stats3.Pruned, stats3.Final)
	stage3Runs := collector.runMany(stage3)
	topConn := topConfigs(stage3Runs, levelCfg.TopConnCount)

	segmentCandidates := []int64{1 * 1024 * 1024, 2 * 1024 * 1024, 4 * 1024 * 1024, 8 * 1024 * 1024, 16 * 1024 * 1024, 32 * 1024 * 1024, 64 * 1024 * 1024}
	stage4 := make([]bench.BenchmarkConfig, 0, len(topConn)*len(segmentCandidates))
	for _, top := range topConn {
		for _, seg := range segmentCandidates {
			stage4 = append(stage4, bench.BenchmarkConfig{TestSizeLabel: sizeLabel, TargetBytes: targetBytes, SourceURL: url, Connections: top.cfg.Connections, QueueMode: true, SegmentSize: seg, BufferSize: defaultBuffer, HTTPMode: top.cfg.HTTPMode, RepeatIndex: 1})
		}
	}
	stage4, stats4 := bench.PruneBenchmarkConfigsWithStats(stage4)
	fmt.Fprintf(out, "Stage4 segments: generated=%d pruned=%d final=%d\n", stats4.Generated, stats4.Pruned, stats4.Final)
	stage4Runs := collector.runMany(stage4)
	topSeg := topConfigs(stage4Runs, levelCfg.TopSegmentKeep)

	stage5 := make([]bench.BenchmarkConfig, 0, len(topSeg)*len(bufferCandidates))
	for _, top := range topSeg {
		for _, bsz := range bufferCandidates {
			stage5 = append(stage5, bench.BenchmarkConfig{TestSizeLabel: sizeLabel, TargetBytes: targetBytes, SourceURL: url, Connections: top.cfg.Connections, QueueMode: true, SegmentSize: top.cfg.SegmentSize, BufferSize: bsz, HTTPMode: top.cfg.HTTPMode, RepeatIndex: 1})
		}
	}
	stage5, stats5 := bench.PruneBenchmarkConfigsWithStats(stage5)
	fmt.Fprintf(out, "Stage5 buffers: generated=%d pruned=%d final=%d\n", stats5.Generated, stats5.Pruned, stats5.Final)
	stage5Runs := collector.runMany(stage5)
	topBuf := topConfigs(stage5Runs, levelCfg.TopBufferKeep)
	finalBase := make([]bench.BenchmarkConfig, 0, len(topBuf))
	for _, t := range topBuf {
		finalBase = append(finalBase, t.cfg)
	}
	finalBase, _ = bench.PruneBenchmarkConfigsWithStats(finalBase)
	if len(finalBase) > levelCfg.FinalKeep {
		finalBase = finalBase[:levelCfg.FinalKeep]
	}

	stage6Results := make([]bench.BenchmarkResult, 0, len(finalBase)*repeats)
	for _, cfg := range finalBase {
		for i := 1; i <= repeats; i++ {
			runCfg := cfg
			runCfg.RepeatIndex = i
			stage6Results = append(stage6Results, collector.run(runCfg))
		}
	}
	ranked := rankAggregates(aggregateByConfig(stage6Results))
	if len(ranked) == 0 {
		fmt.Fprintln(out, "No successful final configs.")
		return nil
	}

	bestOverall := ranked[0]
	var bestAuto *profileTopConfig
	var bestHTTP1 *profileTopConfig
	for i := range ranked {
		r := ranked[i]
		if strings.EqualFold(r.cfg.HTTPMode, "auto") && bestAuto == nil {
			c := r
			bestAuto = &c
		}
		if strings.EqualFold(r.cfg.HTTPMode, "http1") && bestHTTP1 == nil {
			c := r
			bestHTTP1 = &c
		}
	}

	fmt.Fprintln(out, "Stage6 final ranking:")
	printTopConfigSummary(out, "Best overall", bestOverall, repeats)
	if bestAuto != nil {
		printTopConfigSummary(out, "Best http auto", *bestAuto, repeats)
	}
	if bestHTTP1 != nil {
		printTopConfigSummary(out, "Best http1", *bestHTTP1, repeats)
	}
	fmt.Fprintln(out)
	return nil
}

func parseProfileSizes(raw string) ([]string, error) {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	seen := make(map[string]bool)
	sizes := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.ToUpper(strings.TrimSpace(p))
		if v == "" {
			continue
		}
		if _, ok := profileAllowedSizes[v]; !ok {
			return nil, fmt.Errorf("invalid --sizes value %q: allowed 10MB,100MB,1GB", v)
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		sizes = append(sizes, v)
	}
	if len(sizes) == 0 {
		return nil, errors.New("--sizes must include at least one of: 10MB,100MB,1GB")
	}
	return sizes, nil
}

func profileLevelParams(level string) (profileLevelConfig, error) {
	switch level {
	case "quick":
		return profileLevelConfig{ConnCandidates: []int{1, 2, 4, 8}, TopConnCount: 2, TopSegmentKeep: 3, TopBufferKeep: 3, FinalKeep: 3}, nil
	case "normal":
		return profileLevelConfig{ConnCandidates: []int{1, 2, 4, 8, 12, 16, 24, 32}, TopConnCount: 4, TopSegmentKeep: 5, TopBufferKeep: 5, FinalKeep: 5}, nil
	case "exhaustive":
		return profileLevelConfig{ConnCandidates: []int{1, 2, 4, 8, 12, 16, 24, 32}, TopConnCount: 8, TopSegmentKeep: 7, TopBufferKeep: 7, FinalKeep: 5}, nil
	default:
		return profileLevelConfig{}, fmt.Errorf("invalid --level %q: allowed quick, normal, exhaustive", level)
	}
}

func selectProfileURL(customURL string, requiredBytes int64) (string, quickget.ServerTestResult, error) {
	if customURL != "" {
		res, err := quickget.ServerTest(customURL, nil)
		if err != nil {
			return "", quickget.ServerTestResult{}, err
		}
		return customURL, res, nil
	}
	var lastErr error
	for _, u := range profileBuiltInCandidateURLs {
		res, err := quickget.ServerTest(u, nil)
		if err != nil {
			lastErr = err
			continue
		}
		if !res.SupportsRange {
			continue
		}
		if res.ContentLength > 0 && res.ContentLength < requiredBytes {
			continue
		}
		return u, res, nil
	}
	if lastErr != nil {
		return "", quickget.ServerTestResult{}, fmt.Errorf("failed to probe built-in profile URLs: %w", lastErr)
	}
	return "", quickget.ServerTestResult{}, errors.New("no built-in profile URL satisfied requested sizes with range support")
}

func chooseBufferCandidates(url string) []int {
	_ = url
	candidates := append([]int(nil), defaultBufferCandidates...)
	tmp, err := os.CreateTemp("", "quickget-profile-disk-*.tmp")
	if err != nil {
		return candidates
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(tmpPath)
	rec, err := quickget.RecommendBuffer(tmpPath)
	if err == nil && rec.BufferSize > 0 {
		candidates = append(candidates, rec.BufferSize)
	}
	return candidates
}

func defaultSegmentSizeForLabel(label string) int64 {
	switch strings.ToUpper(strings.TrimSpace(label)) {
	case "10MB":
		return 4 * 1024 * 1024
	case "100MB":
		return 8 * 1024 * 1024
	default:
		return 16 * 1024 * 1024
	}
}

func topConfigs(results []bench.BenchmarkResult, keep int) []profileTopConfig {
	ranked := rankAggregates(aggregateByConfig(results))
	if len(ranked) > keep {
		ranked = ranked[:keep]
	}
	return ranked
}

func aggregateByConfig(results []bench.BenchmarkResult) map[string]profileTopConfig {
	grouped := make(map[string][]bench.BenchmarkResult)
	cfgByKey := make(map[string]bench.BenchmarkConfig)
	for _, r := range results {
		cfg := bench.BenchmarkConfig{TestSizeLabel: r.TestSizeLabel, TargetBytes: r.TargetBytes, SourceURL: r.SourceURL, Connections: r.Connections, QueueMode: r.QueueMode, SegmentSize: r.SegmentSize, BufferSize: r.BufferSize, HTTPMode: r.HTTPMode}
		key := configKey(cfg)
		grouped[key] = append(grouped[key], r)
		cfgByKey[key] = cfg
	}
	out := make(map[string]profileTopConfig, len(grouped))
	for key, list := range grouped {
		out[key] = profileTopConfig{cfg: cfgByKey[key], summary: calcStats(list)}
	}
	return out
}

func rankAggregates(aggregates map[string]profileTopConfig) []profileTopConfig {
	ranked := make([]profileTopConfig, 0, len(aggregates))
	for _, v := range aggregates {
		ranked = append(ranked, v)
	}
	sort.Slice(ranked, func(i, j int) bool {
		a := ranked[i].summary
		b := ranked[j].summary
		if a.MedianMBps != b.MedianMBps {
			return a.MedianMBps > b.MedianMBps
		}
		if a.AvgMBps != b.AvgMBps {
			return a.AvgMBps > b.AvgMBps
		}
		if a.StddevMBps != b.StddevMBps {
			return a.StddevMBps < b.StddevMBps
		}
		if a.SuccessRate != b.SuccessRate {
			return a.SuccessRate > b.SuccessRate
		}
		return ranked[i].cfg.Connections < ranked[j].cfg.Connections
	})
	return ranked
}

func calcStats(results []bench.BenchmarkResult) benchStats {
	stats := benchStats{Total: len(results)}
	if len(results) == 0 {
		return stats
	}
	speeds := make([]float64, 0, len(results))
	for _, r := range results {
		if r.Success {
			stats.Successes++
			speeds = append(speeds, r.AvgMBps)
		}
	}
	stats.SuccessRate = float64(stats.Successes) / float64(stats.Total)
	if len(speeds) == 0 {
		return stats
	}
	sort.Float64s(speeds)
	stats.MedianMBps = medianFloat64(speeds)
	var sum float64
	for _, v := range speeds {
		sum += v
	}
	stats.AvgMBps = sum / float64(len(speeds))
	var sq float64
	for _, v := range speeds {
		d := v - stats.AvgMBps
		sq += d * d
	}
	stats.StddevMBps = math.Sqrt(sq / float64(len(speeds)))
	return stats
}

func medianFloat64(sortedVals []float64) float64 {
	if len(sortedVals) == 0 {
		return 0
	}
	m := len(sortedVals) / 2
	if len(sortedVals)%2 == 1 {
		return sortedVals[m]
	}
	return (sortedVals[m-1] + sortedVals[m]) / 2
}

func configKey(cfg bench.BenchmarkConfig) string {
	return fmt.Sprintf("%s|%d|%d|%d|%d|%s", cfg.TestSizeLabel, cfg.TargetBytes, cfg.Connections, cfg.SegmentSize, cfg.BufferSize, strings.ToLower(strings.TrimSpace(cfg.HTTPMode)))
}

func dedupePositiveInts(v []int) []int {
	seen := make(map[int]bool, len(v))
	out := make([]int, 0, len(v))
	for _, x := range v {
		if x <= 0 || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}

func formatBufferList(v []int) string {
	labels := make([]string, 0, len(v))
	for _, b := range v {
		labels = append(labels, formatBytesShort(int64(b)))
	}
	return strings.Join(labels, ",")
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

func printTopConfigSummary(out io.Writer, label string, top profileTopConfig, repeats int) {
	fmt.Fprintf(out, "%s: n=%d seg=%d buf=%d http=%s median=%.2fMB/s avg=%.2fMB/s stddev=%.2f success=%d/%d repeats=%d\n",
		label, top.cfg.Connections, top.cfg.SegmentSize, top.cfg.BufferSize, top.cfg.HTTPMode, top.summary.MedianMBps, top.summary.AvgMBps, top.summary.StddevMBps, top.summary.Successes, top.summary.Total, repeats)
}

func writeRawCSV(path string, results []bench.BenchmarkResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"timestamp", "quickget_version", "os", "arch", "cpu_count", "go_version", "size_label", "target_bytes", "url", "repeat_index", "connections", "queue_mode", "segment_size", "buffer_size", "http_mode", "elapsed_seconds", "avg_mbps", "peak_mbps", "bytes_downloaded", "success", "error", "server_supports_ranges", "disk_write_percent"})
	for _, r := range results {
		peak := ""
		if r.PeakMBps != nil {
			peak = fmt.Sprintf("%.6f", *r.PeakMBps)
		}
		disk := ""
		if r.DiskWritePercent != nil {
			disk = fmt.Sprintf("%.6f", *r.DiskWritePercent)
		}
		rangeSupport := ""
		if r.ServerRangeSupported != nil {
			rangeSupport = strconv.FormatBool(*r.ServerRangeSupported)
		}
		_ = w.Write([]string{r.Timestamp.Format(time.RFC3339), r.QuickGetVersion, r.OS, r.Arch, strconv.Itoa(r.CPUCount), r.GoVersion, r.TestSizeLabel, strconv.FormatInt(r.TargetBytes, 10), r.SourceURL, strconv.Itoa(r.RepeatIndex), strconv.Itoa(r.Connections), strconv.FormatBool(r.QueueMode), strconv.FormatInt(r.SegmentSize, 10), strconv.Itoa(r.BufferSize), r.HTTPMode, fmt.Sprintf("%.6f", r.ElapsedSeconds), fmt.Sprintf("%.6f", r.AvgMBps), peak, strconv.FormatInt(r.BytesDownloaded, 10), strconv.FormatBool(r.Success), r.ErrorMessage, rangeSupport, disk})
	}
	return w.Error()
}

func writeSummaryCSV(path string, results []bench.BenchmarkResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"size_label", "target_bytes", "rank", "connections", "queue_mode", "segment_size", "buffer_size", "http_mode", "repeats", "successful_repeats", "avg_mbps", "median_mbps", "min_mbps", "max_mbps", "stddev_mbps", "success_rate"})

	aggBySize := aggregateBySize(results)
	sizes := make([]string, 0, len(aggBySize))
	for s := range aggBySize {
		sizes = append(sizes, s)
	}
	sort.Strings(sizes)
	for _, s := range sizes {
		ranked := rankAggregates(aggBySize[s])
		for i, x := range ranked {
			minMBps, maxMBps := minMaxMBpsForConfig(results, x.cfg)
			_ = w.Write([]string{x.cfg.TestSizeLabel, strconv.FormatInt(x.cfg.TargetBytes, 10), strconv.Itoa(i + 1), strconv.Itoa(x.cfg.Connections), strconv.FormatBool(x.cfg.QueueMode), strconv.FormatInt(x.cfg.SegmentSize, 10), strconv.Itoa(x.cfg.BufferSize), x.cfg.HTTPMode, strconv.Itoa(x.summary.Total), strconv.Itoa(x.summary.Successes), fmt.Sprintf("%.6f", x.summary.AvgMBps), fmt.Sprintf("%.6f", x.summary.MedianMBps), fmt.Sprintf("%.6f", minMBps), fmt.Sprintf("%.6f", maxMBps), fmt.Sprintf("%.6f", x.summary.StddevMBps), fmt.Sprintf("%.6f", x.summary.SuccessRate)})
		}
	}
	return w.Error()
}

func writeRecommendationsJSON(path, url string, repeats int, sizes []string, results []bench.BenchmarkResult) error {
	aggBySize := aggregateBySize(results)
	recs := profileRecommendations{SourceURL: url, Sizes: make(map[string]profileSizeRecommendation)}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, sizeLabel := range sizes {
		entry := profileSizeRecommendation{TestedAt: now, Repeats: repeats, Notes: "Queue-mode profile results. Failed runs are included in raw results."}
		ranked := rankAggregates(aggBySize[sizeLabel])
		if len(ranked) == 0 {
			entry.Notes = "No successful configurations ranked for this size."
			recs.Sizes[sizeLabel] = entry
			continue
		}
		bestOverall := topToRecommendationEntry(ranked[0])
		entry.BestOverall = &bestOverall
		for _, item := range ranked {
			if entry.BestHTTPAuto == nil && strings.EqualFold(item.cfg.HTTPMode, "auto") {
				v := topToRecommendationEntry(item)
				entry.BestHTTPAuto = &v
			}
			if entry.BestHTTP1 == nil && strings.EqualFold(item.cfg.HTTPMode, "http1") {
				v := topToRecommendationEntry(item)
				entry.BestHTTP1 = &v
			}
		}
		recs.Sizes[sizeLabel] = entry
	}
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func topToRecommendationEntry(top profileTopConfig) profileRecommendationEntry {
	return profileRecommendationEntry{SizeLabel: top.cfg.TestSizeLabel, TargetBytes: top.cfg.TargetBytes, Connections: top.cfg.Connections, QueueMode: top.cfg.QueueMode, SegmentSize: top.cfg.SegmentSize, BufferSize: top.cfg.BufferSize, HTTPMode: top.cfg.HTTPMode, MedianMBps: top.summary.MedianMBps, AvgMBps: top.summary.AvgMBps, StddevMBps: top.summary.StddevMBps, SuccessRate: top.summary.SuccessRate, Successes: top.summary.Successes, TotalRuns: top.summary.Total}
}

func aggregateBySize(results []bench.BenchmarkResult) map[string]map[string]profileTopConfig {
	bySizeRows := make(map[string][]bench.BenchmarkResult)
	for _, r := range results {
		bySizeRows[r.TestSizeLabel] = append(bySizeRows[r.TestSizeLabel], r)
	}
	out := make(map[string]map[string]profileTopConfig, len(bySizeRows))
	for size, rows := range bySizeRows {
		out[size] = aggregateByConfig(rows)
	}
	return out
}

func minMaxMBpsForConfig(results []bench.BenchmarkResult, cfg bench.BenchmarkConfig) (float64, float64) {
	key := configKey(cfg)
	minSet := false
	var minV, maxV float64
	for _, r := range results {
		curCfg := bench.BenchmarkConfig{TestSizeLabel: r.TestSizeLabel, TargetBytes: r.TargetBytes, SourceURL: r.SourceURL, Connections: r.Connections, QueueMode: r.QueueMode, SegmentSize: r.SegmentSize, BufferSize: r.BufferSize, HTTPMode: r.HTTPMode}
		if configKey(curCfg) != key || !r.Success {
			continue
		}
		if !minSet {
			minSet = true
			minV = r.AvgMBps
			maxV = r.AvgMBps
			continue
		}
		if r.AvgMBps < minV {
			minV = r.AvgMBps
		}
		if r.AvgMBps > maxV {
			maxV = r.AvgMBps
		}
	}
	if !minSet {
		return 0, 0
	}
	return minV, maxV
}

func createProfileOutputDir(now time.Time) (string, error) {
	dir := filepath.Join(".quickget", "profiles", now.Format("2006-01-02_15-04-05"))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

func writeProfileArtifacts(out io.Writer, outDir, url string, repeats int, sizes []string, results []bench.BenchmarkResult) error {
	rawPath := filepath.Join(outDir, "raw_results.csv")
	summaryPath := filepath.Join(outDir, "summary.csv")
	recPath := filepath.Join(outDir, "recommendations.json")
	if err := writeRawCSV(rawPath, results); err != nil {
		return err
	}
	if err := writeSummaryCSV(summaryPath, results); err != nil {
		return err
	}
	if err := writeRecommendationsJSON(recPath, url, repeats, sizes, results); err != nil {
		return err
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Profile output folder: %s\n", outDir)
	fmt.Fprintf(out, "Raw CSV: %s\n", rawPath)
	fmt.Fprintf(out, "Summary CSV: %s\n", summaryPath)
	fmt.Fprintf(out, "Recommendations JSON: %s\n", recPath)
	return nil
}

func printProfileUsage(w io.Writer, name string) {
	fmt.Fprintf(w, "Usage: %s profile [options]\n", name)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Profile options:")
	fmt.Fprintf(w, "  --sizes string\n        comma-separated sizes from 10MB,100MB,1GB (default %q)\n", profileDefaultSizes)
	fmt.Fprintln(w, "  --url string")
	fmt.Fprintln(w, "        optional custom test URL (default uses built-in candidate URLs)")
	fmt.Fprintf(w, "  --repeats int\n        repeats per size (default %d)\n", profileDefaultRepeats)
	fmt.Fprintf(w, "  --level string\n        profile level: quick|normal|exhaustive (default %q)\n", profileDefaultLevel)
}

func formatBytesLabel(b int64) string {
	for label, size := range profileAllowedSizes {
		if size == b {
			return label
		}
	}
	return fmt.Sprintf("%d bytes", b)
}
