package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type ServerProbeResult struct {
	URL                    string
	FinalURL               string
	SupportsRange          bool
	SupportsParallelLikely bool
	StatusCode             int
	ContentLength          int64
	AcceptRanges           string
	ContentRange           string
	SuggestedOutputName    string
	SuggestedConnections   int
	Warnings               []string
}

func ProbeServer(rawURL string, client *http.Client) (*ServerProbeResult, error) {
	validatedURL, err := validateURL(rawURL)
	if err != nil {
		return nil, err
	}

	stats, err := FetchRemoteFileStats(client, validatedURL, nil, DefaultUserAgent)
	if err != nil {
		return nil, err
	}

	result := &ServerProbeResult{
		URL:                  validatedURL,
		FinalURL:             stats.FinalURL,
		StatusCode:           stats.StatusCode,
		ContentLength:        stats.Size,
		AcceptRanges:         stats.AcceptRanges,
		SuggestedOutputName:  detectOutputFilename(stats.FinalURL, stats.ContentDisposition),
		SuggestedConnections: 2,
	}

	rangeReq, err := http.NewRequest(http.MethodGet, stats.FinalURL, nil)
	if err != nil {
		return nil, err
	}
	rangeReq.Header.Set("Range", "bytes=0-0")

	rangeResp, err := client.Do(rangeReq)
	if err != nil {
		return nil, err
	}
	defer rangeResp.Body.Close()
	result.ContentRange = strings.TrimSpace(rangeResp.Header.Get("Content-Range"))

	switch rangeResp.StatusCode {
	case http.StatusPartialContent:
		result.SupportsRange = true
		result.SupportsParallelLikely = true
	case http.StatusOK:
		result.SupportsRange = false
		result.SupportsParallelLikely = false
		result.Warnings = append(result.Warnings, "server ignored Range header; multi-connection download is not supported")
	case http.StatusTooManyRequests, http.StatusForbidden, http.StatusServiceUnavailable:
		result.Warnings = append(result.Warnings, "server may reject aggressive parallel downloads; try lowering -n")
	case http.StatusRequestedRangeNotSatisfiable:
		result.Warnings = append(result.Warnings, "server returned invalid range response; file size or range handling may be unreliable")
	}

	if rangeResp.StatusCode == http.StatusOK {
		result.SuggestedConnections = 1
	} else if rangeResp.StatusCode == http.StatusTooManyRequests || rangeResp.StatusCode == http.StatusForbidden || rangeResp.StatusCode == http.StatusServiceUnavailable {
		result.SuggestedConnections = 2
	} else if result.SupportsRange && len(result.Warnings) == 0 {
		result.SuggestedConnections = 8
	}

	return result, nil
}

func runServerTestCommand(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		printServerTestUsage()
		return nil
	}
	if len(args) != 1 {
		printServerTestUsage()
		return fmt.Errorf("server-test requires exactly one URL")
	}
	return runServerTest(args[0])
}

func runServerTest(rawURL string) error {
	client := newHTTPClient(1, DefaultForceHTTP1, DefaultMaxIdleConns, DefaultIdleTimeoutSec)
	result, err := ProbeServer(rawURL, client)
	if err != nil {
		return err
	}

	fmt.Println("Server test:")
	fmt.Println("URL:", result.URL)
	fmt.Println("Final URL:", result.FinalURL)
	fmt.Println("Suggested filename:", result.SuggestedOutputName)
	if result.ContentLength >= 0 {
		fmt.Println("Content-Length:", result.ContentLength)
	} else {
		fmt.Println("Content-Length: unknown")
	}
	if result.AcceptRanges == "" {
		fmt.Println("Accept-Ranges: (none)")
	} else {
		fmt.Println("Accept-Ranges:", result.AcceptRanges)
	}

	rangeState := "failed"
	if result.SupportsRange {
		rangeState = "supported"
	} else if containsWarning(result.Warnings, "server ignored Range header; multi-connection download is not supported") {
		rangeState = "ignored"
	}
	fmt.Println("Range test:", rangeState)

	fmt.Println("Recommendation:")
	if containsWarning(result.Warnings, "server ignored Range header; multi-connection download is not supported") {
		fmt.Println("- Use -n 1 (range ignored)")
	} else if containsWarning(result.Warnings, "server may reject aggressive parallel downloads; try lowering -n") {
		fmt.Println("- Use lower -n like 2 or 4 (server may throttle/reject)")
	} else if result.SupportsRange && len(result.Warnings) == 0 {
		fmt.Println("- Use -n 8 (range supported)")
	} else {
		fmt.Printf("- Start conservatively with -n %d\n", result.SuggestedConnections)
	}

	for _, w := range result.Warnings {
		fmt.Println("- Warning:", w)
	}

	return nil
}

func printServerTestUsage() {
	name := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, "Usage: %s server-test <url>\n", name)
}

func explainServerStatus(statusCode int, ranged bool, byteRange string, rawURL string) error {
	if statusCode >= 200 && statusCode < 300 {
		if ranged && statusCode == http.StatusPartialContent {
			return nil
		}
		if !ranged && statusCode == http.StatusOK {
			return nil
		}
		if ranged && statusCode == http.StatusOK {
			return fmt.Errorf("server ignored Range header for %s (%s); multi-connection download is not supported", rawURL, byteRange)
		}
		if ranged {
			return fmt.Errorf("unexpected success status %d for ranged request %s to %s", statusCode, byteRange, rawURL)
		}
		return nil
	}

	switch statusCode {
	case http.StatusTooManyRequests, http.StatusForbidden, http.StatusServiceUnavailable:
		return fmt.Errorf("server returned %d for %s (%s); try lowering -n (for example 2 or 4)", statusCode, rawURL, byteRange)
	case http.StatusRequestedRangeNotSatisfiable:
		return fmt.Errorf("server returned 416 for %s (%s); file size or range handling may be unreliable", rawURL, byteRange)
	default:
		if ranged {
			return fmt.Errorf("request failed with status %d for ranged request %s to %s", statusCode, byteRange, rawURL)
		}
		return fmt.Errorf("request failed with status %d for %s", statusCode, rawURL)
	}
}

func containsWarning(warnings []string, target string) bool {
	for _, w := range warnings {
		if w == target {
			return true
		}
	}
	return false
}
