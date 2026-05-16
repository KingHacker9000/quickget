package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"quickget/pkg/quickget/agentclient"
	"quickget/pkg/quickget/api"
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
	case "add":
		return runAgentAdd(args[1:], stdout, stderr, binName)
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
	client, err := newAgentClient(args, stderr, binName, "health", false)
	if err != nil {
		return err
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
	client, err := newAgentClient(args, stderr, binName, "list", false)
	if err != nil {
		return err
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
		fmt.Fprintf(stdout, "%s | %s | %.2f%% | %d/%s bytes | %s\n", s.ID, s.Status, s.Percent, s.Downloaded, total, s.OutputPath)
	}
	return nil
}

func runAgentAdd(args []string, stdout io.Writer, stderr io.Writer, binName string) error {
	client, req, err := parseAgentAddArgs(args, stderr, binName)
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
	fs := flag.NewFlagSet("agent delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	deleteFiles := fs.Bool("delete-files", false, "delete downloaded file data when deleting job")
	fs.Usage = func() { printAgentDeleteUsage(stderr, binName) }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		printAgentDeleteUsage(stderr, binName)
		return errors.New("delete requires exactly one id")
	}

	client, err := newAgentClient(nil, stderr, binName, "delete", true)
	if err != nil {
		return err
	}
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
	if len(args) != 1 {
		printAgentIDActionUsage(stderr, binName, action)
		return fmt.Errorf("%s requires exactly one id", action)
	}
	id := strings.TrimSpace(args[0])
	if id == "" {
		return errors.New("id cannot be empty")
	}
	client, err := newAgentClient(nil, stderr, binName, action, true)
	if err != nil {
		return err
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

func parseAgentAddArgs(args []string, stderr io.Writer, binName string) (*agentclient.Client, api.CreateDownloadRequest, error) {
	fs := flag.NewFlagSet("agent add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "output filename")
	dir := fs.String("dir", "", "download directory")
	n := fs.Int("n", 0, "number of parallel connections")
	fs.Usage = func() { printAgentAddUsage(stderr, binName) }

	if err := fs.Parse(args); err != nil {
		return nil, api.CreateDownloadRequest{}, err
	}
	if fs.NArg() != 1 {
		printAgentAddUsage(stderr, binName)
		return nil, api.CreateDownloadRequest{}, errors.New("add requires exactly one URL")
	}
	if *n < 0 {
		return nil, api.CreateDownloadRequest{}, errors.New("-n must be >= 0")
	}
	client, err := newAgentClient(nil, stderr, binName, "add", true)
	if err != nil {
		return nil, api.CreateDownloadRequest{}, err
	}
	req := api.CreateDownloadRequest{
		URL:         strings.TrimSpace(fs.Arg(0)),
		OutputPath:  strings.TrimSpace(*output),
		Directory:   strings.TrimSpace(*dir),
		Connections: *n,
	}
	if req.URL == "" {
		return nil, api.CreateDownloadRequest{}, errors.New("url cannot be empty")
	}
	return client, req, nil
}

func newAgentClient(args []string, stderr io.Writer, binName string, subcmd string, allowNoArgs bool) (*agentclient.Client, error) {
	fs := flag.NewFlagSet("agent "+subcmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	agentURL := fs.String("agent", defaultAgentURL, "quickget-agent base URL")
	fs.Usage = func() { printAgentSubcommandUsage(stderr, binName, subcmd) }
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if !allowNoArgs && fs.NArg() != 0 {
		fs.Usage()
		return nil, errors.New("unexpected positional arguments")
	}
	token, err := loadDefaultAgentToken()
	if err != nil {
		return nil, err
	}
	return agentclient.New(*agentURL, token), nil
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
	fmt.Fprintln(w, "Subcommands: health, list, add, pause, resume, cancel, delete")
}

func printAgentSubcommandUsage(w io.Writer, name string, subcmd string) {
	fmt.Fprintf(w, "Usage: %s agent %s\n", name, subcmd)
}

func printAgentAddUsage(w io.Writer, name string) {
	fmt.Fprintf(w, "Usage: %s agent add [-o <file>] [-dir <dir>] [-n <connections>] <url>\n", name)
}

func printAgentDeleteUsage(w io.Writer, name string) {
	fmt.Fprintf(w, "Usage: %s agent delete [-delete-files] <id>\n", name)
}

func printAgentIDActionUsage(w io.Writer, name string, action string) {
	fmt.Fprintf(w, "Usage: %s agent %s <id>\n", name, action)
}
