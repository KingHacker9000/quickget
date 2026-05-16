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
	ID          string     `json:"id"`
	URL         string     `json:"url"`
	OutputPath  string     `json:"outputPath"`
	Status      string     `json:"status"`
	Downloaded  int64      `json:"downloaded"`
	Total       int64      `json:"total"`
	Percent     float64    `json:"percent"`
	SpeedMBps   float64    `json:"speedMBps"`
	AvgMBps     float64    `json:"avgMBps"`
	Error       string     `json:"error"`
	Message     string     `json:"message"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	CompletedAt *time.Time `json:"completedAt"`
}

type ErrorResponse struct {
	Error      string `json:"error"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion"`
}
