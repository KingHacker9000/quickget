package agent

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"quickget/pkg/quickget/api"
	"quickget/pkg/quickget/core"
)

const (
	JobStatusQueued    = "queued"
	JobStatusRunning   = "running"
	JobStatusPaused    = "paused"
	JobStatusCompleted = "completed"
	JobStatusFailed    = "failed"
	JobStatusCancelled = "cancelled"
	JobStatusDeleted   = "deleted"
)

type DownloadJob struct {
	ID          string
	URL         string
	OutputPath  string
	Directory   string
	Status      string
	Connections int

	Downloaded int64
	Total      int64
	Percent    float64
	SpeedMBps  float64
	AvgMBps    float64
	ActiveJobs int
	Mutations  int64
	Segments   []api.SegmentProgress
	Error      string
	Message    string

	Options core.Request

	CreatedAt   time.Time
	UpdatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}

func NewJobID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err == nil {
		return base64.RawURLEncoding.EncodeToString(buf)
	}

	fallback := make([]byte, 4)
	_, _ = rand.Read(fallback)
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), base64.RawURLEncoding.EncodeToString(fallback))
}

func (j *DownloadJob) Snapshot() api.DownloadSnapshot {
	return api.DownloadSnapshot{
		ID:          j.ID,
		URL:         j.URL,
		OutputPath:  j.OutputPath,
		Status:      j.Status,
		Connections: j.Connections,
		Downloaded:  j.Downloaded,
		Total:       j.Total,
		Percent:     j.Percent,
		SpeedMBps:   j.SpeedMBps,
		AvgMBps:     j.AvgMBps,
		ActiveJobs:  j.ActiveJobs,
		Mutations:   j.Mutations,
		Segments:    append([]api.SegmentProgress(nil), j.Segments...),
		Error:       j.Error,
		Message:     j.Message,
		CreatedAt:   j.CreatedAt,
		UpdatedAt:   j.UpdatedAt,
		CompletedAt: j.CompletedAt,
	}
}

func (j *DownloadJob) MarkStatus(status string) {
	now := time.Now().UTC()
	j.Status = status
	j.UpdatedAt = now

	if status == JobStatusRunning && j.StartedAt == nil {
		j.StartedAt = &now
	}

	switch status {
	case JobStatusCompleted, JobStatusFailed, JobStatusCancelled, JobStatusDeleted:
		j.CompletedAt = &now
	case JobStatusQueued, JobStatusRunning, JobStatusPaused:
		j.CompletedAt = nil
	}
}

func (j *DownloadJob) UpdateProgress(downloaded, total int64, percent, speedMBps, avgMBps float64, message string, activeJobs int, mutations int64, segments []api.SegmentProgress) {
	j.Downloaded = downloaded
	j.Total = total
	j.Percent = percent
	j.SpeedMBps = speedMBps
	j.AvgMBps = avgMBps
	j.Message = message
	j.ActiveJobs = activeJobs
	j.Mutations = mutations
	j.Segments = append(j.Segments[:0], segments...)
	j.UpdatedAt = time.Now().UTC()
}
