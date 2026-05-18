package api

import "time"

type CreateDownloadRequest struct {
	URL         string            `json:"url"`
	OutputPath  string            `json:"outputPath"`
	Directory   string            `json:"directory"`
	Connections int               `json:"connections"`
	QueueMode   bool              `json:"queueMode"`
	SegmentSize int64             `json:"segmentSize"`
	BufferSize  int               `json:"bufferSize"`
	Retries     int               `json:"retries"`
	Headers     map[string]string `json:"headers"`
	UserAgent   string            `json:"userAgent"`
	AutoBuffer  bool              `json:"autoBuffer"`
	HTTP1       bool              `json:"http1"`
}

type DownloadResponse struct {
	ID         string    `json:"id"`
	URL        string    `json:"url"`
	OutputPath string    `json:"outputPath"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type DownloadSnapshot struct {
	ID          string            `json:"id"`
	URL         string            `json:"url"`
	OutputPath  string            `json:"outputPath"`
	Status      string            `json:"status"`
	Connections int               `json:"connections,omitempty"`
	Downloaded  int64             `json:"downloaded"`
	Total       int64             `json:"total"`
	Percent     float64           `json:"percent"`
	SpeedMBps   float64           `json:"speedMBps"`
	AvgMBps     float64           `json:"avgMBps"`
	ActiveJobs  int               `json:"activeJobs,omitempty"`
	Mutations   int64             `json:"mutations,omitempty"`
	Segments    []SegmentProgress `json:"segments,omitempty"`
	Error       string            `json:"error"`
	Message     string            `json:"message"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	CompletedAt *time.Time        `json:"completedAt"`
}

type SegmentProgress struct {
	Index      int    `json:"index"`
	StartByte  int64  `json:"startByte"`
	EndByte    int64  `json:"endByte"`
	Downloaded int64  `json:"downloadedBytesWithinSegment"`
	Status     string `json:"status"`
	WorkerID   int    `json:"workerId,omitempty"`
}

type ErrorResponse struct {
	Error      string `json:"error"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion"`
}

type BrowserCaptureRequest struct {
	Source            string            `json:"source"`
	Browser           string            `json:"browser"`
	URL               string            `json:"url"`
	FinalURL          string            `json:"final_url,omitempty"`
	Referrer          string            `json:"referrer,omitempty"`
	SuggestedFilename string            `json:"suggested_filename,omitempty"`
	MIMEType          string            `json:"mime_type,omitempty"`
	TotalBytes        int64             `json:"total_bytes,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	Cookies           string            `json:"cookies,omitempty"`
	TabTitle          string            `json:"tab_title,omitempty"`
	PageURL           string            `json:"page_url,omitempty"`
	CaptureMode       string            `json:"capture_mode"`
	ChromeDownloadID  int               `json:"chrome_download_id,omitempty"`
	ClientRequestID   string            `json:"client_request_id,omitempty"`
}

type DuplicateInfo struct {
	Found              bool   `json:"found"`
	ExistingOutputPath string `json:"existing_output_path,omitempty"`
	ExistingDownloadID string `json:"existing_download_id,omitempty"`
	FileExists         bool   `json:"file_exists"`
	Size               int64  `json:"size,omitempty"`
}

type BrowserCapture struct {
	ID            string                `json:"id"`
	Status        string                `json:"status"`
	Request       BrowserCaptureRequest `json:"request"`
	CreatedAt     time.Time             `json:"created_at"`
	UpdatedAt     time.Time             `json:"updated_at"`
	Message       string                `json:"message,omitempty"`
	DuplicateInfo *DuplicateInfo        `json:"duplicate_info,omitempty"`
}

type StartCaptureDownloadRequest struct {
	OutputPath      string                 `json:"output_path,omitempty"`
	Directory       string                 `json:"directory,omitempty"`
	DuplicateAction string                 `json:"duplicate_action"`
	Options         *CreateDownloadRequest `json:"options,omitempty"`
}
