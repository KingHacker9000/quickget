package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printGlobalUsage()
		return errors.New("no command or URL provided")
	}

	switch args[0] {
	case "help", "-h", "--help":
		printGlobalUsage()
		return nil
	case "download":
		return runDownload(args[1:])
	case "inspect":
		return runInspectCommand(args[1:])
	case "filestats":
		return runFileStatsCommand(args[1:])
	case "server-test":
		return runServerTestCommand(args[1:])
	case "status":
		return runStatusCommand(args[1:])
	case "clean":
		return runCleanCommand(args[1:])
	case "hash":
		return runHashCommand(args[1:])
	case "disk-test", "tune-disk":
		return runDiskTestCommand(args[1:])
	default:
		return runDownload(args)
	}
}

func runInspectCommand(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		printInspectUsage()
		return nil
	}
	if len(args) != 1 {
		printInspectUsage()
		return errors.New("inspect requires exactly one URL")
	}
	return runInspect(args[0])
}

func runStatusCommand(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		printStatusUsage()
		return nil
	}
	if len(args) != 1 {
		printStatusUsage()
		return errors.New("status requires exactly one output file path")
	}
	return runStatus(args[0])
}

func runCleanCommand(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		printCleanUsage()
		return nil
	}
	if len(args) != 1 {
		printCleanUsage()
		return errors.New("clean requires exactly one output file path")
	}
	return runClean(args[0])
}

func runHashCommand(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		printHashUsage()
		return nil
	}
	if len(args) != 1 {
		printHashUsage()
		return errors.New("hash requires exactly one file path")
	}
	return runHash(args[0])
}

func isHelpArg(v string) bool {
	return v == "-h" || v == "--help" || v == "help"
}

func printGlobalUsage() {
	name := filepath.Base(os.Args[0])
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintf(os.Stderr, "  %s download [options] <url>\n", name)
	fmt.Fprintf(os.Stderr, "  %s inspect <url>\n", name)
	fmt.Fprintf(os.Stderr, "  %s filestats <url>\n", name)
	fmt.Fprintf(os.Stderr, "  %s server-test <url>\n", name)
	fmt.Fprintf(os.Stderr, "  %s status <output-file>\n", name)
	fmt.Fprintf(os.Stderr, "  %s clean <output-file>\n", name)
	fmt.Fprintf(os.Stderr, "  %s hash <file>\n", name)
	fmt.Fprintf(os.Stderr, "  %s disk-test -o <temp-test-file>\n", name)
	fmt.Fprintf(os.Stderr, "  %s tune-disk -o <temp-test-file>\n", name)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Backward compatibility:")
	fmt.Fprintf(os.Stderr, "  %s [options] <url>  (same as download)\n", name)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "Run '%s download -h' for download options.\n", name)
}

func printInspectUsage() {
	name := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, "Usage: %s inspect <url>\n", name)
}

func printStatusUsage() {
	name := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, "Usage: %s status <output-file>\n", name)
}

func printCleanUsage() {
	name := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, "Usage: %s clean <output-file>\n", name)
}

func printHashUsage() {
	name := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, "Usage: %s hash <file>\n", name)
}
