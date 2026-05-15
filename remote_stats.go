package main

import (
	"net/http"
	"strings"
)

type RemoteFileStats struct {
	InputURL           string
	FinalURL           string
	Size               int64
	RangeSupported     bool
	AcceptRanges       string
	Status             string
	StatusCode         int
	ContentDisposition string
}

func FetchRemoteFileStats(client *http.Client, rawURL string, headers http.Header, userAgent string) (RemoteFileStats, error) {
	req, err := http.NewRequest(http.MethodHead, rawURL, nil)
	if err != nil {
		return RemoteFileStats{}, err
	}
	applyHeaders(req, headers, userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return RemoteFileStats{}, err
	}
	defer resp.Body.Close()

	size := parseContentLengthHeader(resp.Header.Get("Content-Length"))

	acceptRanges := strings.TrimSpace(resp.Header.Get("Accept-Ranges"))
	rangeSupported := strings.Contains(strings.ToLower(acceptRanges), "bytes")

	return RemoteFileStats{
		InputURL:           rawURL,
		FinalURL:           resp.Request.URL.String(),
		Size:               size,
		RangeSupported:     rangeSupported,
		AcceptRanges:       acceptRanges,
		Status:             resp.Status,
		StatusCode:         resp.StatusCode,
		ContentDisposition: strings.TrimSpace(resp.Header.Get("Content-Disposition")),
	}, nil
}

func parseContentLengthHeader(raw string) int64 {
	contentLength := strings.TrimSpace(raw)
	if contentLength == "" {
		return -1
	}
	v, err := parseInt64(contentLength)
	if err != nil {
		return -1
	}
	return v
}
