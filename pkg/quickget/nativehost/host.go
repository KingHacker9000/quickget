package nativehost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"quickget/pkg/quickget/agent"
	"quickget/pkg/quickget/agentclient"
	"quickget/pkg/quickget/api"
)

const defaultAgentURL = "http://127.0.0.1:19329"

var loadOrCreateToken = agent.LoadOrCreateToken

type rawRequest struct {
	Type string `json:"type"`
}

type browserCaptureRequest struct {
	Type string `json:"type"`
	api.BrowserCaptureRequest
}

type envelopeCaptureRequest struct {
	Type    string                    `json:"type"`
	Payload api.BrowserCaptureRequest `json:"payload"`
}

type browserCaptureExtensionPayload struct {
	URL                   string `json:"url"`
	FinalURL              string `json:"finalUrl"`
	FinalURLSnake         string `json:"final_url"`
	Referrer              string `json:"referrer"`
	FileName              string `json:"fileName"`
	FileNameSnake         string `json:"file_name"`
	SuggestedFilename     string `json:"suggested_filename"`
	MIMEType              string `json:"mimeType"`
	MIMETypeSnake         string `json:"mime_type"`
	TotalBytes            int64  `json:"totalBytes"`
	TotalBytesSnake       int64  `json:"total_bytes"`
	PageURL               string `json:"pageUrl"`
	PageURLSnake          string `json:"page_url"`
	TabTitle              string `json:"tabTitle"`
	TabTitleSnake         string `json:"tab_title"`
	ChromeDownloadID      int    `json:"chromeDownloadId"`
	ChromeDownloadIDSnake int    `json:"chrome_download_id"`
	Cookies               string `json:"cookies"`
	CaptureMode           string `json:"captureMode"`
	CaptureModeSnake      string `json:"capture_mode"`
	ClientRequestID       string `json:"clientRequestId"`
	ClientRequestIDSnake  string `json:"client_request_id"`
}

type envelopeRawCaptureRequest struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type hostConfig struct {
	QDMExecutablePath   string `json:"qdm_executable_path"`
	AgentExecutablePath string `json:"agent_executable_path"`
}

type Host struct {
	in       io.Reader
	out      io.Writer
	errLog   *log.Logger
	agentURL string
}

func NewHost(in io.Reader, out io.Writer, errWriter io.Writer) *Host {
	return &Host{
		in:       in,
		out:      out,
		errLog:   log.New(errWriter, "quickget-native-host: ", log.LstdFlags),
		agentURL: defaultAgentURL,
	}
}

func (h *Host) Serve(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		payload, err := ReadMessageBytes(h.in)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		var req rawRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			h.errLog.Printf("decode request failed: %v", err)
			_ = WriteMessage(h.out, map[string]any{"type": "error", "ok": false, "message": "invalid request payload"})
			continue
		}
		if err := h.handleRequest(ctx, req.Type, payload); err != nil {
			h.errLog.Printf("handle request type=%q failed: %v", req.Type, err)
			_ = WriteMessage(h.out, map[string]any{
				"type":    "error",
				"ok":      false,
				"message": err.Error(),
			})
		}
	}
}

func (h *Host) handleRequest(ctx context.Context, reqType string, payload []byte) error {
	switch strings.TrimSpace(reqType) {
	case "ping":
		return WriteMessage(h.out, map[string]any{"type": "pong", "ok": true})
	case "status":
		return h.handleStatus(ctx)
	case "browser_capture":
		return h.handleBrowserCapture(ctx, payload)
	case "open_qdm":
		return h.handleOpenQDM(ctx)
	default:
		return WriteMessage(h.out, map[string]any{
			"type":    "error",
			"ok":      false,
			"message": "unknown request type",
		})
	}
}

func (h *Host) handleStatus(ctx context.Context) error {
	agentRunning := h.isAgentRunning(ctx)
	qdmRunning := agentRunning
	resp := map[string]any{
		"type":          "status",
		"ok":            true,
		"agent_running": agentRunning,
		"qdm_running":   qdmRunning,
		"message":       "QuickGet agent status checked",
	}
	if !agentRunning {
		resp["message"] = "QuickGet agent is not running"
	}
	return WriteMessage(h.out, resp)
}

func (h *Host) handleBrowserCapture(ctx context.Context, payload []byte) error {
	var req browserCaptureRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return fmt.Errorf("decode browser_capture payload: %w", err)
	}
	if strings.TrimSpace(req.URL) == "" {
		var wrapped envelopeCaptureRequest
		if err := json.Unmarshal(payload, &wrapped); err == nil {
			req.BrowserCaptureRequest = wrapped.Payload
		}
	}
	if strings.TrimSpace(req.URL) == "" || strings.TrimSpace(req.CaptureMode) == "" {
		var wrappedRaw envelopeRawCaptureRequest
		if err := json.Unmarshal(payload, &wrappedRaw); err == nil && len(wrappedRaw.Payload) > 0 {
			var ext browserCaptureExtensionPayload
			if err := json.Unmarshal(wrappedRaw.Payload, &ext); err == nil {
				if strings.TrimSpace(req.URL) == "" {
					req.URL = strings.TrimSpace(ext.URL)
				}
				if strings.TrimSpace(req.FinalURL) == "" {
					req.FinalURL = strings.TrimSpace(firstNonEmpty(ext.FinalURL, ext.FinalURLSnake))
				}
				if strings.TrimSpace(req.Referrer) == "" {
					req.Referrer = strings.TrimSpace(ext.Referrer)
				}
				if strings.TrimSpace(req.SuggestedFilename) == "" {
					req.SuggestedFilename = strings.TrimSpace(firstNonEmpty(ext.FileName, ext.FileNameSnake, ext.SuggestedFilename))
				}
				if strings.TrimSpace(req.MIMEType) == "" {
					req.MIMEType = strings.TrimSpace(firstNonEmpty(ext.MIMEType, ext.MIMETypeSnake))
				}
				if req.TotalBytes == 0 {
					req.TotalBytes = firstPositiveInt64(ext.TotalBytes, ext.TotalBytesSnake)
				}
				if strings.TrimSpace(req.PageURL) == "" {
					req.PageURL = strings.TrimSpace(firstNonEmpty(ext.PageURL, ext.PageURLSnake))
				}
				if strings.TrimSpace(req.TabTitle) == "" {
					req.TabTitle = strings.TrimSpace(firstNonEmpty(ext.TabTitle, ext.TabTitleSnake))
				}
				if req.ChromeDownloadID == 0 {
					req.ChromeDownloadID = firstNonZeroInt(ext.ChromeDownloadID, ext.ChromeDownloadIDSnake)
				}
				if strings.TrimSpace(req.Cookies) == "" {
					req.Cookies = ext.Cookies
				}
				if strings.TrimSpace(req.CaptureMode) == "" {
					req.CaptureMode = strings.TrimSpace(firstNonEmpty(ext.CaptureMode, ext.CaptureModeSnake))
				}
				if strings.TrimSpace(req.ClientRequestID) == "" {
					req.ClientRequestID = strings.TrimSpace(firstNonEmpty(ext.ClientRequestID, ext.ClientRequestIDSnake))
				}
			}
		}
	}
	if strings.TrimSpace(req.Source) == "" {
		req.Source = "chrome-auto-capture"
	}
	if strings.TrimSpace(req.Browser) == "" {
		req.Browser = "chrome"
	}
	if strings.TrimSpace(req.CaptureMode) == "" {
		req.CaptureMode = "ask"
	}
	if strings.TrimSpace(req.URL) == "" {
		return WriteMessage(h.out, map[string]any{
			"type":    "browser_capture_result",
			"ok":      false,
			"error":   "invalid_capture_request",
			"message": "capture request URL is required",
		})
	}

	if err := h.ensureAgentRunning(ctx); err != nil {
		return WriteMessage(h.out, map[string]any{
			"type":              "browser_capture_result",
			"ok":                false,
			"client_request_id": req.ClientRequestID,
			"error":             err.Error(),
			"message":           err.Error(),
		})
	}

	token, err := loadOrCreateToken()
	if err != nil {
		return err
	}
	client := agentclient.New(h.agentURL, token)
	capture, err := client.CreateCapture(ctx, req.BrowserCaptureRequest)
	if err != nil {
		h.errLog.Printf("capture forward failed type=browser_capture url=%s source=%s browser=%s err=%v", req.URL, req.Source, req.Browser, err)
		msg := formatCaptureForwardError(err)
		return WriteMessage(h.out, map[string]any{
			"type":              "browser_capture_result",
			"ok":                false,
			"client_request_id": req.ClientRequestID,
			"error":             msg,
			"message":           msg,
		})
	}

	return WriteMessage(h.out, map[string]any{
		"type":              "browser_capture_result",
		"ok":                true,
		"client_request_id": req.ClientRequestID,
		"capture_id":        capture.ID,
		"message":           "Sent to QuickGet Download Manager",
	})
}

func (h *Host) handleOpenQDM(ctx context.Context) error {
	err := h.ensureAgentRunning(ctx)
	if err != nil {
		return WriteMessage(h.out, map[string]any{
			"type":    "open_qdm_result",
			"ok":      false,
			"message": err.Error(),
		})
	}
	return WriteMessage(h.out, map[string]any{"type": "open_qdm_result", "ok": true})
}

func (h *Host) isAgentRunning(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.agentURL+"/health", nil)
	if err != nil {
		return false
	}
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (h *Host) ensureAgentRunning(ctx context.Context) error {
	if h.isAgentRunning(ctx) {
		return nil
	}
	if runtime.GOOS != "windows" {
		return errors.New("quickget-agent is not running")
	}

	cfg, _ := loadConfig()
	if strings.TrimSpace(cfg.AgentExecutablePath) != "" {
		if err := startDetached(cfg.AgentExecutablePath, "serve"); err == nil {
			time.Sleep(600 * time.Millisecond)
			if h.isAgentRunning(ctx) {
				return nil
			}
		}
	}
	if strings.TrimSpace(cfg.QDMExecutablePath) != "" {
		if err := startDetached(cfg.QDMExecutablePath); err == nil {
			time.Sleep(1200 * time.Millisecond)
			if h.isAgentRunning(ctx) {
				return nil
			}
		}
	}
	return errors.New("quickget-agent is not running. Start QuickGet Download Manager or configure %AppData%\\QuickGet\\native-host.json")
}

func loadConfig() (hostConfig, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return hostConfig{}, err
	}
	path := filepath.Join(cfgDir, "QuickGet", "native-host.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return hostConfig{}, err
	}
	var cfg hostConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return hostConfig{}, err
	}
	return cfg, nil
}

func startDetached(path string, args ...string) error {
	cmd := exec.Command(path, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Start()
}

func formatCaptureForwardError(err error) string {
	if err == nil {
		return "failed to forward capture to quickget-agent: unknown error"
	}
	detail := strings.TrimSpace(err.Error())
	if detail == "" {
		return "failed to forward capture to quickget-agent: unknown error"
	}
	return fmt.Sprintf("failed to forward capture to quickget-agent: %s", detail)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonZeroInt(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
