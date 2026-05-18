package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"quickget/pkg/quickget/api"
)

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type HealthResponse struct {
	OK      bool   `json:"ok"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type APIError struct {
	StatusCode int
	Response   api.ErrorResponse
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Response.Message != "" {
		if e.Response.Code != "" {
			return fmt.Sprintf("api error (%s): %s", e.Response.Code, e.Response.Message)
		}
		return fmt.Sprintf("api error: %s", e.Response.Message)
	}
	return fmt.Sprintf("api error: status %d", e.StatusCode)
}

func New(baseURL string, token string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:      strings.TrimSpace(token),
		httpClient: &http.Client{},
	}
}

func (c *Client) Health(ctx context.Context) (HealthResponse, error) {
	var out HealthResponse
	err := c.do(ctx, http.MethodGet, "/health", nil, &out)
	return out, err
}

func (c *Client) ListDownloads(ctx context.Context) ([]api.DownloadSnapshot, error) {
	var out []api.DownloadSnapshot
	err := c.do(ctx, http.MethodGet, "/downloads", nil, &out)
	return out, err
}

func (c *Client) CreateDownload(ctx context.Context, req api.CreateDownloadRequest) (api.DownloadSnapshot, error) {
	var out api.DownloadSnapshot
	err := c.do(ctx, http.MethodPost, "/downloads", req, &out)
	return out, err
}

func (c *Client) GetDownload(ctx context.Context, id string) (api.DownloadSnapshot, error) {
	var out api.DownloadSnapshot
	err := c.do(ctx, http.MethodGet, "/downloads/"+id, nil, &out)
	return out, err
}

func (c *Client) Pause(ctx context.Context, id string) (api.DownloadSnapshot, error) {
	var out api.DownloadSnapshot
	err := c.do(ctx, http.MethodPost, "/downloads/"+id+"/pause", nil, &out)
	return out, err
}

func (c *Client) Resume(ctx context.Context, id string) (api.DownloadSnapshot, error) {
	var out api.DownloadSnapshot
	err := c.do(ctx, http.MethodPost, "/downloads/"+id+"/resume", nil, &out)
	return out, err
}

func (c *Client) Cancel(ctx context.Context, id string) (api.DownloadSnapshot, error) {
	var out api.DownloadSnapshot
	err := c.do(ctx, http.MethodPost, "/downloads/"+id+"/cancel", nil, &out)
	return out, err
}

func (c *Client) Delete(ctx context.Context, id string, deleteFiles bool) error {
	body := map[string]bool{"delete_files": deleteFiles}
	return c.do(ctx, http.MethodPost, "/downloads/"+id+"/delete", body, nil)
}

func (c *Client) ListCaptures(ctx context.Context) ([]api.BrowserCapture, error) {
	var out []api.BrowserCapture
	err := c.do(ctx, http.MethodGet, "/captures", nil, &out)
	return out, err
}

func (c *Client) CreateCapture(ctx context.Context, req api.BrowserCaptureRequest) (api.BrowserCapture, error) {
	var out api.BrowserCapture
	err := c.do(ctx, http.MethodPost, "/captures", req, &out)
	return out, err
}

func (c *Client) GetCapture(ctx context.Context, id string) (api.BrowserCapture, error) {
	var out api.BrowserCapture
	err := c.do(ctx, http.MethodGet, "/captures/"+id, nil, &out)
	return out, err
}

func (c *Client) RejectCapture(ctx context.Context, id string) (api.BrowserCapture, error) {
	var out api.BrowserCapture
	err := c.do(ctx, http.MethodPost, "/captures/"+id+"/reject", nil, &out)
	return out, err
}

func (c *Client) StartCaptureDownload(ctx context.Context, id string, req api.StartCaptureDownloadRequest) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodPost, "/captures/"+id+"/start", req, &out)
	return out, err
}

func (c *Client) do(ctx context.Context, method string, path string, body any, out any) error {
	var payload io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		payload = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, payload)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{StatusCode: resp.StatusCode, Response: decodeErrorResponse(resp.Body)}
		return apiErr
	}

	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response body: %w", err)
	}
	return nil
}

func decodeErrorResponse(r io.Reader) api.ErrorResponse {
	type nestedError struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	data, err := io.ReadAll(r)
	if err != nil || len(data) == 0 {
		return api.ErrorResponse{}
	}

	var nested nestedError
	if err := json.Unmarshal(data, &nested); err == nil && (nested.Error.Code != "" || nested.Error.Message != "") {
		return api.ErrorResponse{
			Code:    nested.Error.Code,
			Message: nested.Error.Message,
		}
	}

	var flat api.ErrorResponse
	_ = json.Unmarshal(data, &flat)
	return flat
}
