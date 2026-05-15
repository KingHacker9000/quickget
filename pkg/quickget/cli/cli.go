package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"quickget/pkg/quickget"
	"quickget/pkg/quickget/core"
	"quickget/pkg/quickget/probe"
	"quickget/pkg/quickget/tune"
)

type headerFlags struct {
	values []string
}

func (h *headerFlags) String() string {
	return strings.Join(h.values, ", ")
}

func (h *headerFlags) Set(value string) error {
	h.values = append(h.values, value)
	return nil
}

func Run(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	if len(args) == 0 {
		printGlobalUsage(stderr, binName)
		return errors.New("no command or URL provided")
	}

	switch args[0] {
	case "help", "-h", "--help":
		printGlobalUsage(stderr, binName)
		return nil
	case "download":
		return runDownload(args[1:], stdout, stderr, binName)
	case "inspect":
		return runInspectCommand(args[1:], stdout, stderr, binName)
	case "filestats":
		return runFileStatsCommand(args[1:], stdout, stderr, binName)
	case "server-test":
		return runServerTestCommand(args[1:], stdout, stderr, binName)
	case "status":
		return runStatusCommand(args[1:], stdout, stderr, binName)
	case "clean":
		return runCleanCommand(args[1:], stdout, stderr, binName)
	case "hash":
		return runHashCommand(args[1:], stdout, stderr, binName)
	case "disk-test", "tune-disk":
		return runDiskTestCommand(args[1:], stdout, stderr, binName)
	default:
		return runDownload(args, stdout, stderr, binName)
	}
}

func runDownload(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		printDownloadUsage(stderr, binName)
		return nil
	}

	normalized, err := normalizeDownloadArgs(args)
	if err != nil {
		printDownloadUsage(stderr, binName)
		return err
	}

	options, err := parseDownloadOptions(normalized, stderr, binName)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	options.Stdout = stdout

	_, err = quickget.Download(context.Background(), quickget.DownloadRequest{Options: options})
	return err
}

func parseDownloadOptions(args []string, stderr io.Writer, binName string) (core.Request, error) {
	opts := core.DefaultRequest()

	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	fs.SetOutput(stderr)

	output := fs.String("o", "", "output filename")
	outputDir := fs.String("dir", "", "download directory (optional; defaults to OS Downloads folder)")
	workers := fs.Int("n", core.DefaultParallelConnections, "number of parallel connections")
	retries := fs.Int("retries", 3, "max retries per chunk")
	verbose := fs.Bool("v", false, "verbose progress output (includes per-chunk status)")
	dynamic := fs.Bool("d", core.DefaultDynamicSplitting, "enable dynamic chunk splitting/work stealing")
	minSplitSize := fs.Int64("min-split-size", core.DefaultMinSplitSizeBytes, "minimum range size (bytes) required before dynamic split")
	minDynamicFileSize := fs.Int64("min-dynamic-file-size", core.DefaultMinDynamicFileSizeBytes, "minimum file size (bytes) required to enable dynamic splitting")
	queueMode := fs.Bool("queue-mode", false, "enable queue-based segmented downloading")
	segmentSize := fs.Int64("segment-size", core.DefaultSegmentSizeBytes, "segment size in bytes used by queue mode")
	bufferSize := fs.Int("buffer-size", core.DefaultBufferSizeBytes, "download buffer size in bytes")
	autoBuffer := fs.Bool("auto-buffer", false, "auto-tune buffer size for output disk before download")
	forceHTTP1 := fs.Bool("http1", core.DefaultForceHTTP1, "disable HTTP/2 and force HTTP/1.1 behavior")
	maxIdleConns := fs.Int("max-idle-conns", core.DefaultMaxIdleConns, "max idle connections globally")
	idleTimeoutSec := fs.Int("idle-timeout", core.DefaultIdleTimeoutSec, "idle connection timeout in seconds")
	writeDisk := fs.String("write-disk", "", "disk/volume to measure write time for (e.g. C:)")
	userAgent := fs.String("user-agent", core.DefaultUserAgent, "User-Agent header value")
	var customHeaders headerFlags
	fs.Var(&customHeaders, "H", "custom HTTP header, repeatable (example: -H \"Authorization: Bearer TOKEN\")")

	fs.Usage = func() { printDownloadUsage(stderr, binName) }

	if err := fs.Parse(args); err != nil {
		return opts, err
	}

	if fs.NArg() != 1 {
		printDownloadUsage(stderr, binName)
		return opts, errors.New("exactly one positional URL argument is required")
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
	parsedHeaders, err := parseCustomHeaders(customHeaders.values)
	if err != nil {
		return opts, err
	}
	bufferSizeSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "buffer-size" {
			bufferSizeSet = true
		}
	})

	opts.OutputPath = *output
	opts.OutputDir = strings.TrimSpace(*outputDir)
	opts.Workers = *workers
	opts.Retries = *retries
	opts.Verbose = *verbose
	opts.Dynamic = *dynamic
	opts.MinSplitSize = *minSplitSize
	opts.MinDynamicFileSize = *minDynamicFileSize
	opts.QueueMode = *queueMode
	opts.SegmentSize = *segmentSize
	opts.BufferSize = *bufferSize
	opts.BufferSizeSet = bufferSizeSet
	opts.AutoBuffer = *autoBuffer
	opts.ForceHTTP1 = *forceHTTP1
	opts.MaxIdleConns = *maxIdleConns
	opts.IdleTimeoutSec = *idleTimeoutSec
	opts.WriteDisk = strings.TrimSpace(*writeDisk)
	opts.UserAgent = strings.TrimSpace(*userAgent)
	opts.Headers = parsedHeaders
	opts.URL = fs.Arg(0)

	return opts, nil
}

func parseCustomHeaders(values []string) (http.Header, error) {
	headers := make(http.Header)
	for _, raw := range values {
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid header %q: expected \"Header-Name: value\"", raw)
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("invalid header %q: header name cannot be empty", raw)
		}
		headers.Add(key, val)
	}
	return headers, nil
}

func runInspectCommand(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		printInspectUsage(stderr, binName)
		return nil
	}
	if len(args) != 1 {
		printInspectUsage(stderr, binName)
		return errors.New("inspect requires exactly one URL")
	}
	info, err := quickget.Inspect(args[0], nil)
	if err != nil {
		return fmt.Errorf("HEAD request failed: %w", err)
	}
	fmt.Fprintln(stdout, "Input URL:", info.InputURL)
	fmt.Fprintln(stdout, "Final URL:", info.FinalURL)
	fmt.Fprintln(stdout, "HTTP status:", info.Status)
	if info.Size >= 0 {
		fmt.Fprintln(stdout, "Content-Length:", info.Size)
	} else {
		fmt.Fprintln(stdout, "Content-Length: unknown")
	}
	fmt.Fprintln(stdout, "Accept-Ranges bytes:", info.RangeSupported)
	fmt.Fprintln(stdout, "Suggested filename:", info.SuggestedOutputName)
	return nil
}

func runFileStatsCommand(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		printFileStatsUsage(stderr, binName)
		return nil
	}
	if len(args) != 1 {
		printFileStatsUsage(stderr, binName)
		return errors.New("filestats requires exactly one URL")
	}
	stats, err := quickget.FileStats(args[0], nil)
	if err != nil {
		return fmt.Errorf("HEAD request failed: %w", err)
	}

	suggested := probe.DetectOutputFilename(stats.FinalURL, stats.ContentDisposition)

	fmt.Fprintln(stdout, "Input URL:", stats.InputURL)
	fmt.Fprintln(stdout, "Final URL:", stats.FinalURL)
	fmt.Fprintln(stdout, "HTTP status:", stats.Status)
	if stats.Size >= 0 {
		fmt.Fprintln(stdout, "Content-Length:", stats.Size)
	} else {
		fmt.Fprintln(stdout, "Content-Length: unknown")
	}
	if stats.AcceptRanges == "" {
		fmt.Fprintln(stdout, "Accept-Ranges: (none)")
	} else {
		fmt.Fprintln(stdout, "Accept-Ranges:", stats.AcceptRanges)
	}
	fmt.Fprintln(stdout, "Range supported:", stats.RangeSupported)
	fmt.Fprintln(stdout, "Suggested filename:", suggested)
	return nil
}

func runServerTestCommand(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		printServerTestUsage(stderr, binName)
		return nil
	}
	if len(args) != 1 {
		printServerTestUsage(stderr, binName)
		return fmt.Errorf("server-test requires exactly one URL")
	}
	result, err := quickget.ServerTest(args[0], nil)
	if err != nil {
		return err
	}

	fmt.Fprintln(stdout, "Server test:")
	fmt.Fprintln(stdout, "URL:", result.URL)
	fmt.Fprintln(stdout, "Final URL:", result.FinalURL)
	fmt.Fprintln(stdout, "Suggested filename:", result.SuggestedOutputName)
	if result.ContentLength >= 0 {
		fmt.Fprintln(stdout, "Content-Length:", result.ContentLength)
	} else {
		fmt.Fprintln(stdout, "Content-Length: unknown")
	}
	if result.AcceptRanges == "" {
		fmt.Fprintln(stdout, "Accept-Ranges: (none)")
	} else {
		fmt.Fprintln(stdout, "Accept-Ranges:", result.AcceptRanges)
	}

	rangeState := "failed"
	if result.SupportsRange {
		rangeState = "supported"
	} else if containsWarning(result.Warnings, "server ignored Range header; multi-connection download is not supported") {
		rangeState = "ignored"
	}
	fmt.Fprintln(stdout, "Range test:", rangeState)

	fmt.Fprintln(stdout, "Recommendation:")
	if containsWarning(result.Warnings, "server ignored Range header; multi-connection download is not supported") {
		fmt.Fprintln(stdout, "- Use -n 1 (range ignored)")
	} else if containsWarning(result.Warnings, "server may reject aggressive parallel downloads; try lowering -n") {
		fmt.Fprintln(stdout, "- Use lower -n like 2 or 4 (server may throttle/reject)")
	} else if result.SupportsRange && len(result.Warnings) == 0 {
		fmt.Fprintln(stdout, "- Use -n 8 (range supported)")
	} else {
		fmt.Fprintf(stdout, "- Start conservatively with -n %d\n", result.SuggestedConnections)
	}

	for _, w := range result.Warnings {
		fmt.Fprintln(stdout, "- Warning:", w)
	}
	return nil
}

func runStatusCommand(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		printStatusUsage(stderr, binName)
		return nil
	}
	if len(args) != 1 {
		printStatusUsage(stderr, binName)
		return errors.New("status requires exactly one output file path")
	}
	s, err := quickget.ManifestStatus(args[0])
	if err != nil {
		return err
	}

	fmt.Fprintln(stdout, "Manifest:", s.ManifestPath)
	fmt.Fprintln(stdout, "URL:", s.URL)
	fmt.Fprintln(stdout, "Output:", s.OutputPath)
	fmt.Fprintln(stdout, "Total size:", s.TotalSize)
	fmt.Fprintln(stdout, "Connections:", s.Connections)
	fmt.Fprintln(stdout, "Queue mode:", s.QueueMode)
	if s.QueueMode {
		fmt.Fprintln(stdout, "Segment size:", s.SegmentSize)
	}
	fmt.Fprintf(stdout, "Chunks: %d/%d complete\n", s.DoneChunks, s.Chunks)
	fmt.Fprintf(stdout, "Progress: %d/%d bytes (%.2f%%)\n", s.Downloaded, s.Total, s.Percent)
	fmt.Fprintln(stdout, "State:", s.State)
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Raw manifest JSON:")
	fmt.Fprintln(stdout, s.RawJSON)
	return nil
}

func runCleanCommand(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		printCleanUsage(stderr, binName)
		return nil
	}
	if len(args) != 1 {
		printCleanUsage(stderr, binName)
		return errors.New("clean requires exactly one output file path")
	}
	res, err := quickget.ManifestClean(args[0])
	if err != nil {
		return err
	}
	if res.ManifestWasMissing {
		fmt.Fprintln(stdout, "Manifest not found:", res.ManifestPath)
		fmt.Fprintln(stdout, "Output file unchanged:", args[0])
		return nil
	}
	if res.ManifestRemoved {
		fmt.Fprintln(stdout, "Removed manifest:", res.ManifestPath)
	}
	if res.OutputRemoved {
		fmt.Fprintln(stdout, "Removed partial output file:", args[0])
	}
	if res.OutputMissing {
		fmt.Fprintln(stdout, "Partial output file already missing:", args[0])
	}
	if res.OutputPreserved {
		fmt.Fprintln(stdout, "Download is complete; kept output file:", args[0])
	}
	return nil
}

func runHashCommand(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		printHashUsage(stderr, binName)
		return nil
	}
	if len(args) != 1 {
		printHashUsage(stderr, binName)
		return errors.New("hash requires exactly one file path")
	}
	sum, err := quickget.HashFile(args[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s  %s\n", sum, args[0])
	return nil
}

func runDiskTestCommand(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	fs := flag.NewFlagSet("disk-test", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "temp output test file path")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: %s disk-test -o <temp-test-file>\n", binName)
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *output == "" {
		fs.Usage()
		return errors.New("-o is required")
	}

	fmt.Fprintf(stdout, "Testing disk write performance at: %s\n", filepath.Dir(*output))
	fmt.Fprintf(stdout, "Running %d passes per buffer size (test file: %dMB)\n", tune.DiskTestRepeats, tune.DiskTestFileSizeBytes/1024/1024)
	results, recommended, err := quickget.RunDiskTest(*output)
	if err != nil {
		return err
	}
	for _, r := range results {
		fmt.Fprintf(stdout, "%-6s %7.0f MB/s   avg write %.2fms\n", tune.FormatBytesBinary(r.BufferSize), r.ThroughputMBps, float64(r.AvgWriteLatency.Microseconds())/1000.0)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Recommended buffer size: %s\n", tune.FormatBytesBinary(recommended.BufferSize))
	fmt.Fprintln(stdout, "Reason: best stable throughput with low write latency")
	return nil
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
		"queue-mode":  true,
	}
	valueFlags := map[string]bool{
		"o":                     true,
		"dir":                   true,
		"n":                     true,
		"retries":               true,
		"min-split-size":        true,
		"min-dynamic-file-size": true,
		"segment-size":          true,
		"buffer-size":           true,
		"max-idle-conns":        true,
		"idle-timeout":          true,
		"write-disk":            true,
		"user-agent":            true,
		"H":                     true,
	}

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

func containsWarning(warnings []string, target string) bool {
	for _, w := range warnings {
		if w == target {
			return true
		}
	}
	return false
}

func isHelpArg(v string) bool {
	return v == "-h" || v == "--help" || v == "help"
}

func printGlobalUsage(w io.Writer, name string) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintf(w, "  %s download [options] <url>\n", name)
	fmt.Fprintf(w, "  %s inspect <url>\n", name)
	fmt.Fprintf(w, "  %s filestats <url>\n", name)
	fmt.Fprintf(w, "  %s server-test <url>\n", name)
	fmt.Fprintf(w, "  %s status <output-file>\n", name)
	fmt.Fprintf(w, "  %s clean <output-file>\n", name)
	fmt.Fprintf(w, "  %s hash <file>\n", name)
	fmt.Fprintf(w, "  %s disk-test -o <temp-test-file>\n", name)
	fmt.Fprintf(w, "  %s tune-disk -o <temp-test-file>\n", name)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Backward compatibility:")
	fmt.Fprintf(w, "  %s [options] <url>  (same as download)\n", name)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Run '%s download -h' for download options.\n", name)
}

func printDownloadUsage(w io.Writer, name string) {
	fmt.Fprintf(w, "Usage: %s download [options] <url>\n", name)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Backward compatibility:")
	fmt.Fprintf(w, "  %s [options] <url>\n", name)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Download options:")
	fmt.Fprintln(w, "  -o string")
	fmt.Fprintln(w, "        output filename (optional; auto-detected when omitted)")
	fmt.Fprintln(w, "  -dir string")
	fmt.Fprintln(w, "        download directory (optional; defaults to OS Downloads folder)")
	fmt.Fprintf(w, "  -n int\n        number of parallel connections (default %d)\n", core.DefaultParallelConnections)
	fmt.Fprintln(w, "  -retries int")
	fmt.Fprintln(w, "        max retries per chunk (default 3)")
	fmt.Fprintln(w, "  -v")
	fmt.Fprintln(w, "        verbose progress output (includes per-chunk status)")
	fmt.Fprintf(w, "  -d\n        enable dynamic chunk splitting/work stealing (default %t)\n", core.DefaultDynamicSplitting)
	fmt.Fprintf(w, "  -min-split-size int\n        minimum range size in bytes before dynamic split (default %d)\n", core.DefaultMinSplitSizeBytes)
	fmt.Fprintf(w, "  -min-dynamic-file-size int\n        minimum file size in bytes required for dynamic splitting (default %d)\n", core.DefaultMinDynamicFileSizeBytes)
	fmt.Fprintln(w, "  -queue-mode")
	fmt.Fprintln(w, "        enable queue-based segmented downloading (default false)")
	fmt.Fprintf(w, "  -segment-size int\n        segment size in bytes for queue mode (default %d)\n", core.DefaultSegmentSizeBytes)
	fmt.Fprintf(w, "  -buffer-size int\n        download buffer size in bytes (default %d)\n", core.DefaultBufferSizeBytes)
	fmt.Fprintln(w, "  -auto-buffer")
	fmt.Fprintln(w, "        auto-tune buffer size for output disk before download")
	fmt.Fprintf(w, "  -http1\n        disable HTTP/2 and force HTTP/1.1 behavior (default %t)\n", core.DefaultForceHTTP1)
	fmt.Fprintf(w, "  -max-idle-conns int\n        max idle connections globally (default %d)\n", core.DefaultMaxIdleConns)
	fmt.Fprintf(w, "  -idle-timeout int\n        idle connection timeout in seconds (default %d)\n", core.DefaultIdleTimeoutSec)
	fmt.Fprintln(w, "  -write-disk string")
	fmt.Fprintln(w, "        disk/volume to measure write timing for (example: C:)")
	fmt.Fprintf(w, "  -user-agent string\n        User-Agent header value (default %q)\n", core.DefaultUserAgent)
	fmt.Fprintln(w, "  -H value")
	fmt.Fprintln(w, "        custom HTTP header, repeatable (example: -H \"Authorization: Bearer TOKEN\")")
}

func printInspectUsage(w io.Writer, name string) {
	fmt.Fprintf(w, "Usage: %s inspect <url>\n", name)
}

func printStatusUsage(w io.Writer, name string) {
	fmt.Fprintf(w, "Usage: %s status <output-file>\n", name)
}

func printCleanUsage(w io.Writer, name string) {
	fmt.Fprintf(w, "Usage: %s clean <output-file>\n", name)
}

func printHashUsage(w io.Writer, name string) {
	fmt.Fprintf(w, "Usage: %s hash <file>\n", name)
}

func printFileStatsUsage(w io.Writer, name string) {
	fmt.Fprintf(w, "Usage: %s filestats <url>\n", name)
}

func printServerTestUsage(w io.Writer, name string) {
	fmt.Fprintf(w, "Usage: %s server-test <url>\n", name)
}
