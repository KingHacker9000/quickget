package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"quickget/pkg/quickget/agentclient"
	"quickget/pkg/quickget/api"
	"quickget/pkg/quickget/core"
)

const defaultAgentURL = "http://127.0.0.1:19329"

func runAgentCommand(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printAgentUsage(stderr, binName)
		if len(args) == 0 {
			return errors.New("agent subcommand is required")
		}
		return nil
	}

	switch args[0] {
	case "health":
		return runAgentHealth(args[1:], stdout, stderr, binName)
	case "list":
		return runAgentList(args[1:], stdout, stderr, binName)
	case "download":
		return runAgentDownload(args[1:], stdout, stderr, binName)
	case "add":
		return runAgentDownload(args[1:], stdout, stderr, binName)
	case "pause":
		return runAgentPause(args[1:], stdout, stderr, binName)
	case "resume":
		return runAgentResume(args[1:], stdout, stderr, binName)
	case "cancel":
		return runAgentCancel(args[1:], stdout, stderr, binName)
	case "delete":
		return runAgentDelete(args[1:], stdout, stderr, binName)
	default:
		printAgentUsage(stderr, binName)
		return fmt.Errorf("unknown agent subcommand: %s", args[0])
	}
}

func runAgentHealth(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	client, rest, err := newAgentClient(args, stderr, binName, "health", false)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		printAgentSubcommandUsage(stderr, binName, "health")
		return errors.New("health does not accept positional arguments")
	}
	resp, err := client.Health(context.Background())
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Agent: %s\n", resp.Name)
	fmt.Fprintf(stdout, "Version: %s\n", resp.Version)
	fmt.Fprintf(stdout, "Healthy: %t\n", resp.OK)
	return nil
}

func runAgentList(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	client, rest, err := newAgentClient(args, stderr, binName, "list", true)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		printAgentSubcommandUsage(stderr, binName, "list")
		return errors.New("list does not accept positional arguments")
	}
	items, err := client.ListDownloads(context.Background())
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Fprintln(stdout, "No downloads.")
		return nil
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	for _, s := range items {
		total := "unknown"
		if s.Total >= 0 {
			total = fmt.Sprintf("%d", s.Total)
		}
		line := fmt.Sprintf("%s | %s | %.2f%% | %d/%s bytes | %s", s.ID, s.Status, s.Percent, s.Downloaded, total, s.OutputPath)
		if s.Error != "" {
			line += " | error: " + s.Error
		} else if s.Message != "" {
			line += " | " + s.Message
		}
		fmt.Fprintln(stdout, line)
	}
	return nil
}

func runAgentDownload(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	client, req, err := parseAgentDownloadArgs(args, stderr, binName)
	if err != nil {
		return err
	}
	snap, err := client.CreateDownload(context.Background(), req)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Added: %s\n", snap.ID)
	fmt.Fprintf(stdout, "Status: %s\n", snap.Status)
	fmt.Fprintf(stdout, "Output: %s\n", snap.OutputPath)
	return nil
}

func runAgentPause(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	return runAgentMutateByID(args, stdout, stderr, binName, "pause")
}

func runAgentResume(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	return runAgentMutateByID(args, stdout, stderr, binName, "resume")
}

func runAgentCancel(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	return runAgentMutateByID(args, stdout, stderr, binName, "cancel")
}

func runAgentDelete(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	normalized, err := normalizeAgentArgs(args, map[string]bool{"agent": true}, 1)
	if err != nil {
		printAgentDeleteUsage(stderr, binName)
		return err
	}

	fs := flag.NewFlagSet("agent delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agentURL := fs.String("agent", defaultAgentURL, "quickget-agent base URL")
	deleteFiles := fs.Bool("delete-files", false, "delete downloaded file data when deleting job")
	fs.Usage = func() { printAgentDeleteUsage(stderr, binName) }
	if err := fs.Parse(normalized); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		printAgentDeleteUsage(stderr, binName)
		return errors.New("delete requires exactly one id")
	}

	token, err := loadDefaultAgentToken()
	if err != nil {
		return err
	}
	client := agentclient.New(*agentURL, token)

	id := strings.TrimSpace(fs.Arg(0))
	if id == "" {
		return errors.New("id cannot be empty")
	}
	if err := client.Delete(context.Background(), id, *deleteFiles); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Deleted: %s (delete-files=%t)\n", id, *deleteFiles)
	return nil
}

func runAgentMutateByID(args []string, stdout io.Writer, stderr io.Writer, binName string, action string) error {
	normalized, err := normalizeAgentArgs(args, map[string]bool{"agent": true}, 1)
	if err != nil {
		printAgentIDActionUsage(stderr, binName, action)
		return fmt.Errorf("%s requires exactly one id", action)
	}

	client, rest, err := newAgentClient(normalized, stderr, binName, action, true)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		printAgentIDActionUsage(stderr, binName, action)
		return fmt.Errorf("%s requires exactly one id", action)
	}

	id := strings.TrimSpace(rest[0])
	if id == "" {
		return errors.New("id cannot be empty")
	}

	var snap api.DownloadSnapshot
	switch action {
	case "pause":
		snap, err = client.Pause(context.Background(), id)
	case "resume":
		snap, err = client.Resume(context.Background(), id)
	case "cancel":
		snap, err = client.Cancel(context.Background(), id)
	default:
		return fmt.Errorf("unsupported action: %s", action)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: %s\n", strings.ToUpper(action[:1])+action[1:], snap.ID)
	fmt.Fprintf(stdout, "Status: %s\n", snap.Status)
	return nil
}

func parseAgentDownloadArgs(args []string, stderr io.Writer, binName string) (*agentclient.Client, api.CreateDownloadRequest, error) {
	normalized, err := normalizeAgentArgs(args, map[string]bool{
		"agent":        true,
		"o":            true,
		"dir":          true,
		"n":            true,
		"retries":      true,
		"segment-size": true,
		"buffer-size":  true,
		"user-agent":   true,
		"H":            true,
	}, 1)
	if err != nil {
		printAgentDownloadUsage(stderr, binName)
		return nil, api.CreateDownloadRequest{}, err
	}

	fs := flag.NewFlagSet("agent download", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agentURL := fs.String("agent", defaultAgentURL, "quickget-agent base URL")
	output := fs.String("o", "", "output filename")
	dir := fs.String("dir", "", "download directory")
	n := fs.Int("n", core.DefaultParallelConnections, "number of parallel connections")
	retries := fs.Int("retries", 3, "max retries per chunk")
	queueMode := fs.Bool("queue-mode", false, "enable queue-based segmented downloading")
	segmentSize := fs.Int64("segment-size", core.DefaultSegmentSizeBytes, "segment size in bytes used by queue mode")
	bufferSize := fs.Int("buffer-size", core.DefaultBufferSizeBytes, "download buffer size in bytes")
	autoBuffer := fs.Bool("auto-buffer", false, "auto-tune buffer size for output disk before download")
	http1 := fs.Bool("http1", core.DefaultForceHTTP1, "disable HTTP/2 and force HTTP/1.1 behavior")
	userAgent := fs.String("user-agent", core.DefaultUserAgent, "User-Agent header value")
	var customHeaders headerFlags
	fs.Var(&customHeaders, "H", "custom HTTP header, repeatable (example: -H \"Authorization: Bearer TOKEN\")")
	fs.Usage = func() { printAgentDownloadUsage(stderr, binName) }

	if err := fs.Parse(normalized); err != nil {
		return nil, api.CreateDownloadRequest{}, err
	}
	if fs.NArg() != 1 {
		printAgentDownloadUsage(stderr, binName)
		return nil, api.CreateDownloadRequest{}, errors.New("download requires exactly one URL")
	}
	if *n < 0 {
		return nil, api.CreateDownloadRequest{}, errors.New("-n must be >= 0")
	}
	if *retries < 0 {
		return nil, api.CreateDownloadRequest{}, errors.New("-retries must be >= 0")
	}
	if *segmentSize <= 0 {
		return nil, api.CreateDownloadRequest{}, errors.New("-segment-size must be > 0")
	}
	if *bufferSize <= 0 {
		return nil, api.CreateDownloadRequest{}, errors.New("-buffer-size must be > 0")
	}
	parsedHeaders, err := parseCustomHeaders(customHeaders.values)
	if err != nil {
		return nil, api.CreateDownloadRequest{}, err
	}
	headers := make(map[string]string, len(parsedHeaders))
	for key := range parsedHeaders {
		if value := strings.TrimSpace(parsedHeaders.Get(key)); value != "" {
			headers[key] = value
		}
	}

	token, err := loadDefaultAgentToken()
	if err != nil {
		return nil, api.CreateDownloadRequest{}, err
	}
	client := agentclient.New(*agentURL, token)

	req := api.CreateDownloadRequest{
		URL:         strings.TrimSpace(fs.Arg(0)),
		OutputPath:  strings.TrimSpace(*output),
		Directory:   strings.TrimSpace(*dir),
		Connections: *n,
		QueueMode:   *queueMode,
		SegmentSize: *segmentSize,
		BufferSize:  *bufferSize,
		Retries:     *retries,
		Headers:     headers,
		UserAgent:   strings.TrimSpace(*userAgent),
		AutoBuffer:  *autoBuffer,
		HTTP1:       *http1,
	}
	if req.URL == "" {
		return nil, api.CreateDownloadRequest{}, errors.New("url cannot be empty")
	}
	if req.OutputPath == "" {
		req.OutputPath = deriveSafeOutputFilenameFromURL(req.URL)
	}
	return client, req, nil
}

func deriveSafeOutputFilenameFromURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "download.bin"
	}
	base := strings.TrimSpace(path.Base(parsed.Path))
	if decoded, decodeErr := url.PathUnescape(base); decodeErr == nil {
		base = decoded
	}
	base = sanitizeFilename(base)
	if base == "" || base == "." || base == "/" {
		return "download.bin"
	}
	return base
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r < 32:
			continue
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(strings.Trim(b.String(), ". "))
	if out == "" {
		return ""
	}
	upper := strings.ToUpper(out)
	switch upper {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return "_" + out
	}
	return out
}

func newAgentClient(args []string, stderr io.Writer, binName string, subcmd string, requireToken bool) (*agentclient.Client, []string, error) {
	fs := flag.NewFlagSet("agent "+subcmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	agentURL := fs.String("agent", defaultAgentURL, "quickget-agent base URL")
	fs.Usage = func() { printAgentSubcommandUsage(stderr, binName, subcmd) }
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	token := ""
	if requireToken {
		var err error
		token, err = loadDefaultAgentToken()
		if err != nil {
			return nil, nil, err
		}
	}
	return agentclient.New(*agentURL, token), fs.Args(), nil
}

func loadDefaultAgentToken() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(configDir, "QuickGet", "agent-token")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("agent token file not found: %s", path)
		}
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("agent token file is empty")
	}
	return token, nil
}

func printAgentUsage(w io.Writer, name string) {
	fmt.Fprintf(w, "Usage: %s agent <subcommand> [options]\n", name)
	fmt.Fprintln(w, "Subcommands: health, list, download, pause, resume, cancel, delete")
}

func printAgentSubcommandUsage(w io.Writer, name string, subcmd string) {
	fmt.Fprintf(w, "Usage: %s agent %s\n", name, subcmd)
}

func printAgentDownloadUsage(w io.Writer, name string) {
	fmt.Fprintf(w, "Usage: %s agent download [-agent <url>] [-o <file>] [-dir <dir>] [-n <connections>] [download options] <url>\n", name)
}

func printAgentDeleteUsage(w io.Writer, name string) {
	fmt.Fprintf(w, "Usage: %s agent delete [-agent <url>] [-delete-files] <id>\n", name)
}

func printAgentIDActionUsage(w io.Writer, name string, action string) {
	fmt.Fprintf(w, "Usage: %s agent %s [-agent <url>] <id>\n", name, action)
}

func normalizeAgentArgs(args []string, valueFlags map[string]bool, expectedPositionals int) ([]string, error) {
	if valueFlags == nil {
		valueFlags = map[string]bool{}
	}

	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, expectedPositionals)

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
			}
			continue
		}
		positionals = append(positionals, tok)
	}

	if len(positionals) != expectedPositionals {
		return nil, fmt.Errorf("expected %d positional argument(s), got %d", expectedPositionals, len(positionals))
	}
	return append(flags, positionals...), nil
}
