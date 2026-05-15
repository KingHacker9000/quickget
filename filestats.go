package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func runFileStatsCommand(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		printFileStatsUsage()
		return nil
	}
	if len(args) != 1 {
		printFileStatsUsage()
		return errors.New("filestats requires exactly one URL")
	}
	return runFileStats(args[0])
}

func runFileStats(rawURL string) error {
	validatedURL, err := validateURL(rawURL)
	if err != nil {
		return err
	}

	client := newHTTPClient(1, DefaultForceHTTP1, DefaultMaxIdleConns, DefaultIdleTimeoutSec)
	stats, err := FetchRemoteFileStats(client, validatedURL, nil, DefaultUserAgent)
	if err != nil {
		return fmt.Errorf("HEAD request failed: %w", err)
	}

	suggested := detectOutputFilename(stats.FinalURL, stats.ContentDisposition)

	fmt.Println("Input URL:", stats.InputURL)
	fmt.Println("Final URL:", stats.FinalURL)
	fmt.Println("HTTP status:", stats.Status)
	if stats.Size >= 0 {
		fmt.Println("Content-Length:", stats.Size)
	} else {
		fmt.Println("Content-Length: unknown")
	}
	if stats.AcceptRanges == "" {
		fmt.Println("Accept-Ranges: (none)")
	} else {
		fmt.Println("Accept-Ranges:", stats.AcceptRanges)
	}
	fmt.Println("Range supported:", stats.RangeSupported)
	fmt.Println("Suggested filename:", suggested)
	return nil
}

func printFileStatsUsage() {
	name := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, "Usage: %s filestats <url>\n", name)
}

