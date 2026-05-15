package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Chunk struct {
	Index      int
	Start      int64
	End        int64
	Downloaded int64
	Done       bool
}

func splitIntoChunks(size int64, connections int) []Chunk {
	if size <= 0 || connections <= 0 {
		return nil
	}

	chunks := make([]Chunk, 0, connections)
	baseSize := size / int64(connections)
	remainder := size % int64(connections)

	start := int64(0)
	for i := 0; i < connections; i++ {
		partSize := baseSize
		if int64(i) < remainder {
			partSize++
		}

		end := start + partSize - 1
		chunks = append(chunks, Chunk{
			Index:      i,
			Start:      start,
			End:        end,
			Downloaded: 0,
			Done:       false,
		})
		start = end + 1
	}

	// Ensure the final chunk always ends exactly at size-1.
	if len(chunks) > 0 {
		chunks[len(chunks)-1].End = size - 1
	}

	return chunks
}

func preallocateFile(outputPath string, size int64) error {
	if size < 0 {
		return fmt.Errorf("invalid file size: %d", size)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	return f.Truncate(size)
}

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

func downloadSingle(rawURL string, outputPath string, totalSize int64) error {
	resp, err := http.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET failed with status: %s", resp.Status)
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()

	buf := make([]byte, 32*1024)
	var downloaded int64
	start := time.Now()
	lastPrint := start
	lastBytes := int64(0)

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			downloaded += int64(n)
		}

		now := time.Now()
		if now.Sub(lastPrint) >= time.Second || (readErr == io.EOF && downloaded > 0) {
			interval := now.Sub(lastPrint).Seconds()
			if interval <= 0 {
				interval = 1
			}
			speedMBps := float64(downloaded-lastBytes) / (1024 * 1024) / interval
			downloadedMB := float64(downloaded) / (1024 * 1024)

			if totalSize > 0 {
				totalMB := float64(totalSize) / (1024 * 1024)
				percent := (float64(downloaded) / float64(totalSize)) * 100
				fmt.Printf("\rDownloaded: %.2f MB / %.2f MB (%.1f%%) Speed: %.2f MB/s", downloadedMB, totalMB, percent, speedMBps)
			} else {
				fmt.Printf("\rDownloaded: %.2f MB / ? MB (?%%) Speed: %.2f MB/s", downloadedMB, speedMBps)
			}

			lastPrint = now
			lastBytes = downloaded
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	fmt.Println()
	return nil
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

	chunks := splitIntoChunks(size, *workers)
	if len(chunks) == 0 {
		fmt.Println("Chunks: unavailable (file size unknown or invalid)")
	} else {
		fmt.Println("Chunks:")
		for _, c := range chunks {
			fmt.Printf("  [%d] %d-%d\n", c.Index, c.Start, c.End)
		}
	}

	if size >= 0 {
		if err := preallocateFile(*out, size); err != nil {
			fmt.Fprintf(os.Stderr, "error: preallocate failed: %v\n", err)
			os.Exit(1)
		}
	}

	if err := downloadSingle(finalURL, *out, size); err != nil {
		fmt.Fprintf(os.Stderr, "error: download failed: %v\n", err)
		os.Exit(1)
	}
}
