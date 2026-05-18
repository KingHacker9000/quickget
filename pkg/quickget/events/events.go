package events

import (
	"encoding/json"
	"io"
	"sync"
	"time"

	"quickget/pkg/quickget/api"
)

const (
	EventDownloadCreated       = "download.created"
	EventDownloadStarted       = "download.started"
	EventDownloadProgress      = "download.progress"
	EventDownloadWarning       = "download.warning"
	EventDownloadPaused        = "download.paused"
	EventDownloadCancelled     = "download.cancelled"
	EventDownloadCompleted     = "download.completed"
	EventDownloadFailed        = "download.failed"
	EventAgentReady            = "agent.ready"
	EventProfilerStarted       = "profiler.started"
	EventProfilerStage         = "profiler.stage"
	EventProfilerLog           = "profiler.log"
	EventProfilerCompleted     = "profiler.completed"
	EventProfilerFailed        = "profiler.failed"
	EventProfilerCancelled     = "profiler.cancelled"
	EventCaptureRequested      = "capture.requested"
	EventCaptureRejected       = "capture.rejected"
	EventCaptureStarted        = "capture.started"
	EventCaptureDuplicateFound = "capture.duplicate_found"
)

type Event struct {
	Type        string                `json:"type"`
	ID          string                `json:"id"`
	Timestamp   time.Time             `json:"timestamp"`
	Downloaded  int64                 `json:"downloaded"`
	Total       int64                 `json:"total"`
	Percent     float64               `json:"percent"`
	SpeedMBps   float64               `json:"speedMBps"`
	AvgMBps     float64               `json:"avgMBps"`
	Status      string                `json:"status"`
	Connections int                   `json:"connections,omitempty"`
	ActiveJobs  int                   `json:"activeJobs,omitempty"`
	Mutations   int64                 `json:"mutations,omitempty"`
	Segments    []api.SegmentProgress `json:"segments,omitempty"`
	Message     string                `json:"message"`
	Error       string                `json:"error"`
	Suggestion  string                `json:"suggestion"`
	Data        map[string]any        `json:"data,omitempty"`
}

type Emitter struct {
	w  io.Writer
	mu sync.Mutex
}

func NewEmitter(w io.Writer) *Emitter {
	return &Emitter{w: w}
}

func (e *Emitter) Emit(event any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = e.w.Write(data)
	return err
}
