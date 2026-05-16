package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"quickget/pkg/quickget"
	"quickget/pkg/quickget/api"
	"quickget/pkg/quickget/core"
	"quickget/pkg/quickget/events"
	"quickget/pkg/quickget/manifest"
	"quickget/pkg/quickget/store"
)

type Store interface {
	Load() (store.AgentState, error)
	Save(state store.AgentState) error
}

type Downloader interface {
	Download(ctx context.Context, opts quickget.DownloadOptions, emit quickget.EventCallback) error
}

type coreDownloader struct{}

func (coreDownloader) Download(ctx context.Context, opts quickget.DownloadOptions, emit quickget.EventCallback) error {
	return quickget.Download(ctx, opts, emit)
}

type runControl struct {
	cancel          context.CancelFunc
	pauseRequested  bool
	cancelRequested bool
}

type Manager struct {
	mu                 sync.RWMutex
	jobs               map[string]*DownloadJob
	running            map[string]*runControl
	bus                *EventBus
	store              Store
	dl                 Downloader
	progressIntervalMs int
}

func NewManager(store Store) *Manager {
	return &Manager{
		jobs:               make(map[string]*DownloadJob),
		running:            make(map[string]*runControl),
		bus:                NewEventBus(),
		store:              store,
		dl:                 coreDownloader{},
		progressIntervalMs: readAgentProgressIntervalMs(),
	}
}

func (m *Manager) SetDownloader(dl Downloader) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dl == nil {
		m.dl = coreDownloader{}
		return
	}
	m.dl = dl
}

func (m *Manager) CreateDownload(req api.CreateDownloadRequest) (api.DownloadSnapshot, error) {
	if req.URL == "" {
		return api.DownloadSnapshot{}, errors.New("url is required")
	}

	req.URL = strings.TrimSpace(req.URL)
	req.Directory = strings.TrimSpace(req.Directory)
	req.OutputPath = strings.TrimSpace(req.OutputPath)
	if req.OutputPath == "." {
		req.OutputPath = ""
	}
	if req.OutputPath == "" {
		req.OutputPath = deriveSafeOutputFilenameFromURL(req.URL)
	}

	now := time.Now().UTC()
	coreReq := toCoreRequest(req)
	resolvedOutputPath := resolveJobOutputPath(coreReq.OutputPath, coreReq.OutputDir)
	job := &DownloadJob{
		ID:         NewJobID(),
		URL:        req.URL,
		OutputPath: resolvedOutputPath,
		Directory:  req.Directory,
		Status:     JobStatusQueued,
		CreatedAt:  now,
		UpdatedAt:  now,
		Options:    coreReq,
	}

	m.mu.Lock()
	m.jobs[job.ID] = job
	snap := job.Snapshot()
	m.mu.Unlock()

	log.Printf("agent: created job id=%s url=%s output=%s dir=%s", job.ID, job.URL, job.OutputPath, job.Directory)
	m.publish(job, events.EventDownloadCreated, "", "")
	_ = m.SaveState()

	if err := m.Start(job.ID); err != nil {
		return api.DownloadSnapshot{}, err
	}
	return snap, nil
}

func (m *Manager) Start(id string) error {
	m.mu.Lock()
	job, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("job not found: %s", id)
	}
	if _, running := m.running[id]; running {
		m.mu.Unlock()
		return fmt.Errorf("job already running: %s", id)
	}
	if job.Status == JobStatusCompleted || job.Status == JobStatusDeleted {
		m.mu.Unlock()
		return fmt.Errorf("job cannot be started from status %s", job.Status)
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.running[id] = &runControl{cancel: cancel}
	job.Error = ""
	job.MarkStatus(JobStatusRunning)
	snapshot := job.Snapshot()
	m.mu.Unlock()

	log.Printf("agent: starting job id=%s output=%s dir=%s", snapshot.ID, snapshot.OutputPath, job.Directory)
	m.publishSnapshot(snapshot, events.EventDownloadStarted, "download started", "")
	_ = m.SaveState()

	go m.runDownload(ctx, id)
	return nil
}

func (m *Manager) Pause(id string) error {
	m.mu.Lock()
	job, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("job not found: %s", id)
	}
	control, running := m.running[id]
	if !running {
		m.mu.Unlock()
		return fmt.Errorf("job not running: %s", id)
	}

	control.pauseRequested = true
	cancel := control.cancel
	job.MarkStatus(JobStatusPaused)
	snapshot := job.Snapshot()
	m.mu.Unlock()

	cancel()
	m.publishSnapshot(snapshot, events.EventDownloadPaused, "download paused", "")
	_ = m.SaveState()
	return nil
}

func (m *Manager) Resume(id string) error {
	m.mu.RLock()
	job, ok := m.jobs[id]
	if !ok {
		m.mu.RUnlock()
		return fmt.Errorf("job not found: %s", id)
	}
	status := job.Status
	m.mu.RUnlock()

	if status != JobStatusPaused && status != JobStatusFailed && status != JobStatusCancelled && status != JobStatusQueued {
		return fmt.Errorf("job cannot be resumed from status %s", status)
	}

	return m.Start(id)
}

func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	job, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("job not found: %s", id)
	}

	control, running := m.running[id]
	if running {
		control.cancelRequested = true
	}
	job.MarkStatus(JobStatusCancelled)
	snapshot := job.Snapshot()
	var cancel context.CancelFunc
	if running {
		cancel = control.cancel
	}
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	m.publishSnapshot(snapshot, events.EventDownloadCancelled, "download cancelled", "")
	_ = m.SaveState()
	return nil
}

func (m *Manager) Delete(id string, deleteFiles bool) error {
	m.mu.Lock()
	job, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("job not found: %s", id)
	}
	if control, running := m.running[id]; running {
		control.cancelRequested = true
		control.cancel()
		delete(m.running, id)
	}
	delete(m.jobs, id)
	m.mu.Unlock()

	if deleteFiles {
		if outputPath := strings.TrimSpace(job.OutputPath); outputPath != "" {
			_ = os.Remove(outputPath)
			_ = os.Remove(manifest.Path(outputPath))
		}
	}

	e := events.Event{Type: events.EventDownloadCancelled, ID: id, Timestamp: time.Now().UTC(), Status: JobStatusDeleted, Message: "download deleted"}
	m.bus.Publish(e)
	_ = m.SaveState()
	return nil
}

func (m *Manager) Get(id string) (api.DownloadSnapshot, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[id]
	if !ok {
		return api.DownloadSnapshot{}, false
	}
	return job.Snapshot(), true
}

func (m *Manager) List() []api.DownloadSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]api.DownloadSnapshot, 0, len(m.jobs))
	for _, job := range m.jobs {
		out = append(out, job.Snapshot())
	}
	return out
}

func (m *Manager) Events() *EventBus {
	return m.bus
}

func (m *Manager) LoadState() error {
	if m.store == nil {
		return nil
	}

	state, err := m.store.Load()
	if err != nil {
		return err
	}

	m.mu.Lock()
	normalized := false

	m.jobs = make(map[string]*DownloadJob, len(state.Downloads))
	for _, snap := range state.Downloads {
		status := snap.Status
		message := snap.Message
		if status == JobStatusRunning {
			status = JobStatusPaused
			message = "Agent stopped while this download was running. Resume to continue."
			normalized = true
		}

		job := &DownloadJob{
			ID:          snap.ID,
			URL:         snap.URL,
			OutputPath:  snap.OutputPath,
			Directory:   filepath.Dir(snap.OutputPath),
			Status:      status,
			Downloaded:  snap.Downloaded,
			Total:       snap.Total,
			Percent:     snap.Percent,
			SpeedMBps:   snap.SpeedMBps,
			AvgMBps:     snap.AvgMBps,
			Error:       snap.Error,
			Message:     message,
			CreatedAt:   snap.CreatedAt,
			UpdatedAt:   snap.UpdatedAt,
			CompletedAt: snap.CompletedAt,
		}
		if snap.URL != "" {
			outBase := strings.TrimSpace(filepath.Base(snap.OutputPath))
			outDir := strings.TrimSpace(filepath.Dir(snap.OutputPath))
			if outBase == "." || outBase == string(filepath.Separator) {
				outBase = ""
			}
			if outDir == "." || outDir == string(filepath.Separator) {
				outDir = ""
			}
			job.Options = toCoreRequest(api.CreateDownloadRequest{
				URL:        snap.URL,
				OutputPath: outBase,
				Directory:  outDir,
			})
		}
		m.jobs[job.ID] = job
	}
	m.mu.Unlock()

	if normalized {
		if err := m.SaveState(); err != nil {
			return fmt.Errorf("save normalized state: %w", err)
		}
	}

	m.bus.Publish(events.Event{Type: events.EventAgentReady, Timestamp: time.Now().UTC(), Status: "ready", Message: "agent state loaded"})
	return nil
}

func (m *Manager) SaveState() error {
	if m.store == nil {
		return nil
	}

	m.mu.RLock()
	snaps := make([]api.DownloadSnapshot, 0, len(m.jobs))
	for _, job := range m.jobs {
		snaps = append(snaps, job.Snapshot())
	}
	m.mu.RUnlock()

	return m.store.Save(store.AgentState{
		Version:   1,
		Downloads: snaps,
		UpdatedAt: time.Now().UTC(),
	})
}

func (m *Manager) runDownload(ctx context.Context, id string) {
	m.mu.RLock()
	job, ok := m.jobs[id]
	if !ok {
		m.mu.RUnlock()
		return
	}
	req := toDownloadOptions(job)
	req.ProgressIntervalMs = m.progressIntervalMs
	log.Printf("agent: job goroutine started id=%s url=%s output=%s dir=%s", id, req.URL, req.OutputPath, req.Directory)
	m.mu.RUnlock()

	m.mu.RLock()
	dl := m.dl
	m.mu.RUnlock()

	lastProgressSave := time.Time{}
	savedProgressSnapshot := false
	err := dl.Download(ctx, req, func(ev quickget.DownloadEvent) {
		if ev.Type != "progress" {
			return
		}

		m.mu.Lock()
		job, exists := m.jobs[id]
		if !exists {
			m.mu.Unlock()
			return
		}
		job.UpdateProgress(ev.Downloaded, ev.Total, ev.Percent, ev.SpeedMBps, ev.AvgMBps, ev.Message)
		snap := job.Snapshot()
		m.mu.Unlock()

		log.Printf("agent: progress id=%s downloaded=%d total=%d percent=%.2f", id, snap.Downloaded, snap.Total, snap.Percent)
		m.publishSnapshot(snap, events.EventDownloadProgress, ev.Message, "")
		now := time.Now().UTC()
		if !savedProgressSnapshot || now.Sub(lastProgressSave) >= time.Duration(progressPersistIntervalMs)*time.Millisecond {
			_ = m.SaveState()
			lastProgressSave = now
			savedProgressSnapshot = true
		}
	})

	m.mu.Lock()
	job, exists := m.jobs[id]
	if !exists {
		delete(m.running, id)
		m.mu.Unlock()
		return
	}
	control := m.running[id]
	delete(m.running, id)

	pauseRequested := control != nil && control.pauseRequested
	cancelRequested := control != nil && control.cancelRequested

	var eventType string
	var message string
	var errText string

	if err == nil {
		job.MarkStatus(JobStatusCompleted)
		job.Error = ""
		eventType = events.EventDownloadCompleted
		message = "download completed"
		log.Printf("agent: completed job id=%s downloaded=%d total=%d output=%s", id, job.Downloaded, job.Total, job.OutputPath)
	} else if pauseRequested {
		job.MarkStatus(JobStatusPaused)
		eventType = events.EventDownloadPaused
		message = "download paused"
		log.Printf("agent: paused job id=%s", id)
	} else if cancelRequested {
		job.MarkStatus(JobStatusCancelled)
		eventType = events.EventDownloadCancelled
		message = "download cancelled"
		log.Printf("agent: cancelled job id=%s", id)
	} else {
		job.MarkStatus(JobStatusFailed)
		job.Error = err.Error()
		eventType = events.EventDownloadFailed
		message = "download failed"
		errText = err.Error()
		log.Printf("agent: failed job id=%s err=%v", id, err)
	}

	snap := job.Snapshot()
	m.mu.Unlock()

	m.publishSnapshot(snap, eventType, message, errText)
	_ = m.SaveState()
}

func (m *Manager) publish(job *DownloadJob, eventType, msg, errText string) {
	if job == nil {
		return
	}
	m.publishSnapshot(job.Snapshot(), eventType, msg, errText)
}

func (m *Manager) publishSnapshot(s api.DownloadSnapshot, eventType, msg, errText string) {
	m.bus.Publish(events.Event{
		Type:       eventType,
		ID:         s.ID,
		Timestamp:  time.Now().UTC(),
		Downloaded: s.Downloaded,
		Total:      s.Total,
		Percent:    s.Percent,
		SpeedMBps:  s.SpeedMBps,
		AvgMBps:    s.AvgMBps,
		Status:     s.Status,
		Message:    msg,
		Error:      errText,
	})
}

func toCoreRequest(req api.CreateDownloadRequest) core.Request {
	coreReq := core.DefaultRequest()
	coreReq.URL = strings.TrimSpace(req.URL)
	if output := strings.TrimSpace(req.OutputPath); output != "" {
		coreReq.OutputPath = output
	}
	if dir := strings.TrimSpace(req.Directory); dir != "" {
		coreReq.OutputDir = dir
	}
	if req.Connections > 0 {
		coreReq.Workers = req.Connections
	}
	if req.Retries > 0 {
		coreReq.Retries = req.Retries
	}
	coreReq.QueueMode = req.QueueMode
	if req.SegmentSize > 0 {
		coreReq.SegmentSize = req.SegmentSize
	}
	if req.BufferSize > 0 {
		coreReq.BufferSize = req.BufferSize
		coreReq.BufferSizeSet = true
	}
	coreReq.AutoBuffer = req.AutoBuffer
	if req.HTTP1 {
		coreReq.ForceHTTP1 = true
	}
	if userAgent := strings.TrimSpace(req.UserAgent); userAgent != "" {
		coreReq.UserAgent = userAgent
	}
	coreReq.Headers = make(http.Header)
	for k, v := range req.Headers {
		if strings.TrimSpace(k) == "" {
			continue
		}
		coreReq.Headers.Set(k, v)
	}

	return coreReq
}

func toDownloadOptions(job *DownloadJob) quickget.DownloadOptions {
	headers := make(map[string]string)
	for k, values := range job.Options.Headers {
		if len(values) == 0 {
			continue
		}
		headers[k] = values[0]
	}

	outputPath := strings.TrimSpace(job.Options.OutputPath)
	if outputPath == "." {
		outputPath = ""
	}
	if outputPath == "" {
		outputPath = deriveSafeOutputFilenameFromURL(job.URL)
	}

	directory := strings.TrimSpace(job.Options.OutputDir)
	if directory == "." {
		directory = ""
	}

	return quickget.DownloadOptions{
		URL:                job.URL,
		OutputPath:         outputPath,
		Directory:          directory,
		Connections:        job.Options.Workers,
		Retries:            job.Options.Retries,
		QueueMode:          job.Options.QueueMode,
		SegmentSize:        job.Options.SegmentSize,
		BufferSize:         job.Options.BufferSize,
		AutoBuffer:         job.Options.AutoBuffer,
		HTTP1:              job.Options.ForceHTTP1,
		MaxIdleConns:       job.Options.MaxIdleConns,
		IdleTimeoutSeconds: job.Options.IdleTimeoutSec,
		Headers:            headers,
		UserAgent:          job.Options.UserAgent,
		Dynamic:            job.Options.Dynamic,
		MinSplitSize:       job.Options.MinSplitSize,
		MinDynamicFileSize: job.Options.MinDynamicFileSize,
		WriteDisk:          job.Options.WriteDisk,
	}
}

func resolveJobOutputPath(outputPath string, outputDir string) string {
	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return ""
	}
	if filepath.IsAbs(outputPath) {
		return filepath.Clean(outputPath)
	}

	outputDir = strings.TrimSpace(outputDir)
	if outputDir == "" || outputDir == "." {
		outputDir = core.DefaultDownloadDir()
	}

	return filepath.Clean(filepath.Join(outputDir, outputPath))
}
