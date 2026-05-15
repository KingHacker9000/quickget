package main

import (
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

type URLInfo struct {
	InputURL            string
	FinalURL            string
	Size                int64
	RangeSupported      bool
	Status              string
	StatusCode          int
	ContentDisposition  string
	SuggestedOutputName string
}

func runInspect(rawURL string) error {
	validatedURL, err := validateURL(rawURL)
	if err != nil {
		return err
	}

	client := newHTTPClient(1, DefaultForceHTTP1, DefaultMaxIdleConns, DefaultIdleTimeoutSec)
	info, err := fetchURLInfo(client, validatedURL)
	if err != nil {
		return fmt.Errorf("HEAD request failed: %w", err)
	}

	fmt.Println("Input URL:", info.InputURL)
	fmt.Println("Final URL:", info.FinalURL)
	fmt.Println("HTTP status:", info.Status)
	if info.Size >= 0 {
		fmt.Println("Content-Length:", info.Size)
	} else {
		fmt.Println("Content-Length: unknown")
	}
	fmt.Println("Accept-Ranges bytes:", info.RangeSupported)
	fmt.Println("Suggested filename:", info.SuggestedOutputName)
	return nil
}

func fetchURLInfo(client *http.Client, rawURL string) (URLInfo, error) {
	req, err := http.NewRequest(http.MethodHead, rawURL, nil)
	if err != nil {
		return URLInfo{}, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return URLInfo{}, err
	}
	defer resp.Body.Close()

	size := int64(-1)
	contentLength := strings.TrimSpace(resp.Header.Get("Content-Length"))
	if contentLength != "" {
		if v, parseErr := parseInt64(contentLength); parseErr == nil {
			size = v
		}
	}

	acceptRanges := strings.ToLower(resp.Header.Get("Accept-Ranges"))
	rangeSupported := strings.Contains(acceptRanges, "bytes")
	contentDisposition := strings.TrimSpace(resp.Header.Get("Content-Disposition"))
	suggestedName := detectOutputFilename(resp.Request.URL.String(), contentDisposition)

	return URLInfo{
		InputURL:            rawURL,
		FinalURL:            resp.Request.URL.String(),
		Size:                size,
		RangeSupported:      rangeSupported,
		Status:              resp.Status,
		StatusCode:          resp.StatusCode,
		ContentDisposition:  contentDisposition,
		SuggestedOutputName: suggestedName,
	}, nil
}

func validateURL(rawURL string) (string, error) {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid URL: %q", rawURL)
	}
	return parsed.String(), nil
}

func parseInt64(v string) (int64, error) {
	return strconv.ParseInt(v, 10, 64)
}

func detectOutputFilename(rawURL string, contentDisposition string) string {
	if contentDisposition != "" {
		_, params, err := mime.ParseMediaType(contentDisposition)
		if err == nil {
			if filename := strings.TrimSpace(params["filename"]); filename != "" {
				return path.Base(filename)
			}
		}
	}

	parsed, err := url.Parse(rawURL)
	if err == nil {
		base := strings.TrimSpace(path.Base(parsed.Path))
		if base != "" && base != "." && base != "/" {
			return base
		}
	}

	return "download.bin"
}
