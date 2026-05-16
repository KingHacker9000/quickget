package events

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

const (
	EventDownloadCreated   = "download.created"
	EventDownloadStarted   = "download.started"
	EventDownloadProgress  = "download.progress"
	EventDownloadWarning   = "download.warning"
	EventDownloadPaused    = "download.paused"
	EventDownloadCancelled = "download.cancelled"
	EventDownloadCompleted = "download.completed"
	EventDownloadFailed    = "download.failed"
	EventAgentReady        = "agent.ready"
)

type Event struct {
	Type       string    `json:"type"`
	ID         string    `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	Downloaded int64     `json:"downloaded"`
	Total      int64     `json:"total"`
	Percent    float64   `json:"percent"`
	SpeedMBps  float64   `json:"speedMBps"`
	AvgMBps    float64   `json:"avgMBps"`
	Status     string    `json:"status"`
	Message    string    `json:"message"`
	Error      string    `json:"error"`
	Suggestion string    `json:"suggestion"`
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
