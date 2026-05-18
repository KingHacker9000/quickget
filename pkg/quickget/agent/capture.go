package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"quickget/pkg/quickget/api"
)

const (
	CaptureStatusPending  = "pending"
	CaptureStatusAccepted = "accepted"
	CaptureStatusRejected = "rejected"
	CaptureStatusExpired  = "expired"
	CaptureStatusStarted  = "started"
)

func validateCaptureRequest(req api.BrowserCaptureRequest) error {
	if strings.TrimSpace(req.URL) == "" {
		return errors.New("url is required")
	}
	mode := strings.ToLower(strings.TrimSpace(req.CaptureMode))
	if mode != "ask" && mode != "auto" {
		return errors.New("capture_mode must be ask or auto")
	}
	return nil
}

func detectCaptureDuplicate(req api.BrowserCaptureRequest, jobs map[string]*DownloadJob) *api.DuplicateInfo {
	info := &api.DuplicateInfo{Found: false}
	name := strings.TrimSpace(req.SuggestedFilename)
	if name == "" {
		return info
	}

	for _, job := range jobs {
		if strings.EqualFold(filepath.Base(job.OutputPath), name) {
			info.Found = true
			info.ExistingOutputPath = job.OutputPath
			info.ExistingDownloadID = job.ID
			break
		}
	}

	defaultPath := resolveJobOutputPath(name, "")
	if st, err := os.Stat(defaultPath); err == nil && !st.IsDir() {
		info.Found = true
		info.FileExists = true
		info.ExistingOutputPath = defaultPath
		info.Size = st.Size()
	}

	return info
}

func newCapture(id string, req api.BrowserCaptureRequest, duplicate *api.DuplicateInfo) api.BrowserCapture {
	now := time.Now().UTC()
	return api.BrowserCapture{
		ID:            id,
		Status:        CaptureStatusPending,
		Request:       req,
		CreatedAt:     now,
		UpdatedAt:     now,
		Message:       "capture pending user decision",
		DuplicateInfo: duplicate,
	}
}
