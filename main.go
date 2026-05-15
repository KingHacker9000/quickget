package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// getFileInfo performs a HEAD request to the given URL and returns the final URL after redirects,
// the content length (or -1 if not available), whether byte-range requests are supported, and any error encountered.
func getFileInfo(rawURL string) (finalURL string, size int64, rangeSupported bool, err error) {

	size = -1

	req, err := http.NewRequest(http.MethodHead, rawURL, nil)
	if err != nil {
		return "", -1, false, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", -1, false, err
	}
	defer resp.Body.Close()

	finalURL = resp.Request.URL.String()

	contentLength := strings.TrimSpace(resp.Header.Get("Content-Length"))
	if contentLength != "" {
		if v, parseErr := strconv.ParseInt(contentLength, 10, 64); parseErr == nil {
			size = v
		}
	}

	acceptRanges := strings.ToLower(resp.Header.Get("Accept-Ranges"))
	rangeSupported = strings.Contains(acceptRanges, "bytes")

	return finalURL, size, rangeSupported, nil
}

func main() {
	out := flag.String("o", "", "output filename")
	workers := flag.Int("n", 8, "number of parallel connections")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] <url>\n\n", os.Args[0])
		fmt.Fprintln(flag.CommandLine.Output(), "Options:")
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "error: exactly one positional URL argument is required")
		flag.Usage()
		os.Exit(1)
	}

	rawURL := flag.Arg(0)
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		fmt.Fprintf(os.Stderr, "error: invalid URL: %q\n", rawURL)
		os.Exit(1)
	}

	if *workers <= 0 {
		fmt.Fprintln(os.Stderr, "error: -n must be greater than 0")
		os.Exit(1)
	}

	if *out == "" {
		fmt.Fprintln(os.Stderr, "error: -o output filename is required")
		os.Exit(1)
	}

	fmt.Println("URL:", parsed.String())
	fmt.Println("Output:", *out)
	fmt.Println("Parallel connections:", *workers)

	finalURL, size, rangeSupported, err := getFileInfo(parsed.String())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: HEAD request failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Final URL:", finalURL)
	fmt.Println("File size:", size)
	fmt.Println("Range supported:", rangeSupported)
}
