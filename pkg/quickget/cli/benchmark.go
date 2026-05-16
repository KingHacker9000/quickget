package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"quickget/pkg/quickget/bench"
)

func runBenchmarkCommand(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		printBenchmarkUsage(stderr, binName)
		return nil
	}

	fs := newFlagSetWithErrorOutput("benchmark", stderr)
	url := fs.String("url", "", "custom test URL")
	sizesRaw := fs.String("sizes", "10MB,100MB", "comma-separated test sizes")
	repeats := fs.Int("repeats", profileDefaultRepeats, "repeat count")
	connectionsRaw := fs.String("connections", "1,2,4,8", "comma-separated connection counts")
	segmentsRaw := fs.String("segment-sizes", "4MB,8MB,16MB", "comma-separated segment sizes")
	buffersRaw := fs.String("buffer-sizes", "256KB,1MB,4MB", "comma-separated buffer sizes")
	httpModesRaw := fs.String("http-modes", "auto,http1", "comma-separated HTTP modes")
	outputDir := fs.String("output-dir", "", "output directory for benchmark artifacts")
	fs.Usage = func() { printBenchmarkUsage(stderr, binName) }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("benchmark does not accept positional arguments")
	}
	if *repeats <= 0 {
		return fmt.Errorf("--repeats must be > 0")
	}

	sizes, err := parseProfileSizes(*sizesRaw)
	if err != nil {
		return err
	}
	connections, err := parseIntList(*connectionsRaw)
	if err != nil {
		return fmt.Errorf("invalid --connections: %w", err)
	}
	segmentSizes, err := parseByteSizeList(*segmentsRaw)
	if err != nil {
		return fmt.Errorf("invalid --segment-sizes: %w", err)
	}
	bufferSizesRaw, err := parseByteSizeList(*buffersRaw)
	if err != nil {
		return fmt.Errorf("invalid --buffer-sizes: %w", err)
	}
	bufferSizes := make([]int, 0, len(bufferSizesRaw))
	for _, b := range bufferSizesRaw {
		if b > int64(int(^uint(0)>>1)) {
			return fmt.Errorf("buffer size too large: %d", b)
		}
		bufferSizes = append(bufferSizes, int(b))
	}
	httpModes, err := parseHTTPModes(*httpModesRaw)
	if err != nil {
		return err
	}

	maxBytes := int64(0)
	for _, s := range sizes {
		if b := profileAllowedSizes[s]; b > maxBytes {
			maxBytes = b
		}
	}
	selectedURL, probeRes, err := selectProfileURL(strings.TrimSpace(*url), maxBytes)
	if err != nil {
		return err
	}
	if probeRes.ContentLength > 0 && probeRes.ContentLength < maxBytes {
		return fmt.Errorf("selected URL is too small for requested size %s: file size=%d bytes", formatBytesLabel(maxBytes), probeRes.ContentLength)
	}
	if !probeRes.SupportsRange {
		return fmt.Errorf("selected URL does not support byte-range requests: %s", selectedURL)
	}

	base := make([]bench.BenchmarkConfig, 0, len(sizes)*len(connections)*len(segmentSizes)*len(bufferSizes)*len(httpModes))
	for _, sizeLabel := range sizes {
		targetBytes := profileAllowedSizes[sizeLabel]
		for _, n := range connections {
			for _, seg := range segmentSizes {
				for _, buf := range bufferSizes {
					for _, mode := range httpModes {
						base = append(base, bench.BenchmarkConfig{
							TestSizeLabel: sizeLabel,
							TargetBytes:   targetBytes,
							SourceURL:     selectedURL,
							Connections:   n,
							QueueMode:     true,
							SegmentSize:   seg,
							BufferSize:    buf,
							HTTPMode:      mode,
						})
					}
				}
			}
		}
	}

	pruned, stats := bench.PruneBenchmarkConfigsWithStats(base)
	totalRuns := len(pruned) * (*repeats)
	fmt.Fprintf(stdout, "Benchmark matrix: generated=%d pruned=%d final=%d total_runs=%d\n", stats.Generated, stats.Pruned, stats.Final, totalRuns)
	if totalRuns == 0 {
		return fmt.Errorf("no runnable benchmark configs after pruning")
	}

	collector := &profileRunCollector{}
	runConfigs := make([]bench.BenchmarkConfig, 0, totalRuns)
	for _, cfg := range pruned {
		for i := 1; i <= *repeats; i++ {
			x := cfg
			x.RepeatIndex = i
			runConfigs = append(runConfigs, x)
		}
	}
	for i, cfg := range runConfigs {
		res := collector.run(cfg)
		status := "ok"
		if !res.Success {
			status = "failed"
		}
		fmt.Fprintf(stdout, "Run %d/%d | size=%s n=%d segment=%s buffer=%s http=%s repeat=%d/%d | %s\n",
			i+1, len(runConfigs), cfg.TestSizeLabel, cfg.Connections, formatBytesShort(cfg.SegmentSize), formatBytesShort(int64(cfg.BufferSize)), cfg.HTTPMode, cfg.RepeatIndex, *repeats, status)
	}

	aggregates := aggregateByConfig(collector.results)
	ranked := rankAggregates(aggregates)
	if len(ranked) > 0 {
		fmt.Fprintln(stdout, "Top 10 configs:")
		limit := 10
		if len(ranked) < limit {
			limit = len(ranked)
		}
		for i := 0; i < limit; i++ {
			printTopConfigSummary(stdout, fmt.Sprintf("#%d", i+1), ranked[i], *repeats)
		}
	}

	targetDir := strings.TrimSpace(*outputDir)
	if targetDir == "" {
		targetDir, err = createProfileOutputDir(time.Now())
		if err != nil {
			return err
		}
	} else {
		if err := ensureDir(targetDir); err != nil {
			return err
		}
	}
	rawPath := joinPath(targetDir, "raw_results.csv")
	summaryPath := joinPath(targetDir, "summary.csv")
	if err := writeRawCSV(rawPath, collector.results); err != nil {
		return err
	}
	if err := writeSummaryCSV(summaryPath, collector.results); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Benchmark output folder: %s\n", targetDir)
	fmt.Fprintf(stdout, "Raw CSV: %s\n", rawPath)
	fmt.Fprintf(stdout, "Summary CSV: %s\n", summaryPath)
	return nil
}

func parseIntList(raw string) ([]int, error) {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	seen := make(map[int]bool)
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, err
		}
		if n <= 0 || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Ints(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("empty list")
	}
	return out, nil
}

func parseByteSizeList(raw string) ([]int64, error) {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	seen := make(map[int64]bool)
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		n, err := parseByteSize(strings.TrimSpace(p))
		if err != nil {
			return nil, err
		}
		if n <= 0 || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	if len(out) == 0 {
		return nil, fmt.Errorf("empty list")
	}
	return out, nil
}

func parseByteSize(raw string) (int64, error) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	var mult int64 = 1
	switch {
	case strings.HasSuffix(s, "KB"):
		mult = 1024
		s = strings.TrimSuffix(s, "KB")
	case strings.HasSuffix(s, "MB"):
		mult = 1024 * 1024
		s = strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "GB"):
		mult = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, err
	}
	return n * mult, nil
}

func parseHTTPModes(raw string) ([]string, error) {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	seen := make(map[string]bool)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		m := strings.ToLower(strings.TrimSpace(p))
		if m == "" || seen[m] {
			continue
		}
		if m != "auto" && m != "http1" {
			return nil, fmt.Errorf("invalid --http-modes value %q", m)
		}
		seen[m] = true
		out = append(out, m)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("empty list")
	}
	return out, nil
}

func printBenchmarkUsage(w io.Writer, name string) {
	fmt.Fprintf(w, "Usage: %s benchmark [options]\n", name)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Benchmark options:")
	fmt.Fprintln(w, "  --url string")
	fmt.Fprintln(w, "        custom test URL (optional; built-in candidates used when omitted)")
	fmt.Fprintln(w, "  --sizes string")
	fmt.Fprintln(w, "        comma-separated sizes from 10MB,100MB,1GB (default \"10MB,100MB\")")
	fmt.Fprintf(w, "  --repeats int\n        repeats per config (default %d)\n", profileDefaultRepeats)
	fmt.Fprintln(w, "  --connections string")
	fmt.Fprintln(w, "        comma-separated connection counts (default \"1,2,4,8\")")
	fmt.Fprintln(w, "  --segment-sizes string")
	fmt.Fprintln(w, "        comma-separated segment sizes (default \"4MB,8MB,16MB\")")
	fmt.Fprintln(w, "  --buffer-sizes string")
	fmt.Fprintln(w, "        comma-separated buffer sizes (default \"256KB,1MB,4MB\")")
	fmt.Fprintln(w, "  --http-modes string")
	fmt.Fprintln(w, "        comma-separated HTTP modes: auto,http1")
	fmt.Fprintln(w, "  --output-dir string")
	fmt.Fprintln(w, "        output directory for raw_results.csv and summary.csv")
}

func newFlagSetWithErrorOutput(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

func joinPath(dir, name string) string {
	return filepath.Join(dir, name)
}
