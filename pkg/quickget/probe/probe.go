package probe

import (
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

const DefaultUserAgent = "QuickGet/1.0"

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

type HeaderApplier func(req *http.Request, headers http.Header, userAgent string)

func FetchRemoteFileStats(client *http.Client, rawURL string, headers http.Header, userAgent string, applyHeaders HeaderApplier) (RemoteFileStats, error) {
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

func FetchURLInfo(client *http.Client, rawURL string, headers http.Header, userAgent string, applyHeaders HeaderApplier) (URLInfo, error) {
	stats, err := FetchRemoteFileStats(client, rawURL, headers, userAgent, applyHeaders)
	if err != nil {
		return URLInfo{}, err
	}
	suggestedName := DetectOutputFilename(stats.FinalURL, stats.ContentDisposition)

	return URLInfo{
		InputURL:            rawURL,
		FinalURL:            stats.FinalURL,
		Size:                stats.Size,
		RangeSupported:      stats.RangeSupported,
		Status:              stats.Status,
		StatusCode:          stats.StatusCode,
		ContentDisposition:  stats.ContentDisposition,
		SuggestedOutputName: suggestedName,
	}, nil
}

func ProbeServer(rawURL string, client *http.Client, applyHeaders HeaderApplier) (*ServerProbeResult, error) {
	validatedURL, err := ValidateURL(rawURL)
	if err != nil {
		return nil, err
	}

	stats, err := FetchRemoteFileStats(client, validatedURL, nil, DefaultUserAgent, applyHeaders)
	if err != nil {
		return nil, err
	}

	result := &ServerProbeResult{
		URL:                  validatedURL,
		FinalURL:             stats.FinalURL,
		StatusCode:           stats.StatusCode,
		ContentLength:        stats.Size,
		AcceptRanges:         stats.AcceptRanges,
		SuggestedOutputName:  DetectOutputFilename(stats.FinalURL, stats.ContentDisposition),
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

func ExplainServerStatus(statusCode int, ranged bool, byteRange string, rawURL string) error {
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

func ValidateURL(rawURL string) (string, error) {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid URL: %q", rawURL)
	}
	return parsed.String(), nil
}

func DetectOutputFilename(rawURL string, contentDisposition string) string {
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

func ParseInt64(v string) (int64, error) {
	return strconv.ParseInt(v, 10, 64)
}

func parseContentLengthHeader(raw string) int64 {
	contentLength := strings.TrimSpace(raw)
	if contentLength == "" {
		return -1
	}
	v, err := ParseInt64(contentLength)
	if err != nil {
		return -1
	}
	return v
}
