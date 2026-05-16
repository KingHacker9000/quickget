package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"quickget/pkg/quickget"
	"quickget/pkg/quickget/api"
	"quickget/pkg/quickget/events"
	"quickget/pkg/quickget/manifest"
	"quickget/pkg/quickget/store"
)

type fakeStore struct {
	mu      sync.Mutex
	saves   []store.AgentState
	load    store.AgentState
	loadErr error
	saveErr error
}

func (s *fakeStore) Load() (store.AgentState, error) {
	if s.loadErr != nil {
		return store.AgentState{}, s.loadErr
	}
	return s.load, nil
}

func (s *fakeStore) Save(state store.AgentState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saves = append(s.saves, state)
	return nil
}

func (s *fakeStore) saveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.saves)
}

func (s *fakeStore) hasSavedProgress(id string, downloaded int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, state := range s.saves {
		for _, snap := range state.Downloads {
			if snap.ID == id && snap.Downloaded == downloaded {
				return true
			}
		}
	}
	return false
}

type fakeDownloader struct {
	mu       sync.Mutex
	started  chan struct{}
	release  chan struct{}
	progress []quickget.DownloadEvent
	lastOpts quickget.DownloadOptions
	err      error
	block    bool
	calls    int
}

func newFakeDownloader() *fakeDownloader {
	return &fakeDownloader{started: make(chan struct{}), release: make(chan struct{})}
}

func (f *fakeDownloader) Download(ctx context.Context, opts quickget.DownloadOptions, emit quickget.EventCallback) error {
	f.mu.Lock()
	f.calls++
	f.lastOpts = opts
	progress := append([]quickget.DownloadEvent(nil), f.progress...)
	err := f.err
	block := f.block
	started := f.started
	release := f.release
	f.mu.Unlock()

	select {
	case <-started:
	default:
		close(started)
	}

	for _, ev := range progress {
		emit(ev)
	}

	if block {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
		}
	}

	return err
}

func (f *fakeDownloader) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-f.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for downloader start")
	}
}

func (f *fakeDownloader) allowReturn() {
	close(f.release)
}

func (f *fakeDownloader) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeDownloader) options() quickget.DownloadOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastOpts
}

func newManagerWithFake(t *testing.T, dl *fakeDownloader, st *fakeStore) *Manager {
	t.Helper()
	m := NewManager(st)
	m.SetDownloader(dl)
	return m
}

func newReq() api.CreateDownloadRequest {
	return api.CreateDownloadRequest{URL: "https://unit.test/file.bin", OutputPath: "file.bin", Directory: "."}
}

func waitForStatus(t *testing.T, m *Manager, id, status string) api.DownloadSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s, ok := m.Get(id)
		if ok && s.Status == status {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for status %s", status)
	return api.DownloadSnapshot{}
}

func waitEvent(t *testing.T, ch <-chan events.Event, eventType string) events.Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type == eventType {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for event %s", eventType)
		}
	}
}

func TestManagerCreateStartsAndCompletes(t *testing.T) {
	dl := newFakeDownloader()
	st := &fakeStore{}
	m := newManagerWithFake(t, dl, st)

	sub, unsub := m.Events().Subscribe()
	defer unsub()

	snap, err := m.CreateDownload(newReq())
	if err != nil {
		t.Fatalf("CreateDownload error: %v", err)
	}
	if snap.ID == "" {
		t.Fatal("expected job ID")
	}

	dl.waitStarted(t)
	if _, ok := m.Get(snap.ID); !ok {
		t.Fatal("expected job to exist")
	}

	dl.allowReturn()
	final := waitForStatus(t, m, snap.ID, JobStatusCompleted)
	if final.CompletedAt == nil {
		t.Fatal("expected completed timestamp")
	}

	_ = waitEvent(t, sub, events.EventDownloadCreated)
	_ = waitEvent(t, sub, events.EventDownloadStarted)
	_ = waitEvent(t, sub, events.EventDownloadCompleted)
}

func TestManagerProgressUpdatesJobState(t *testing.T) {
	dl := newFakeDownloader()
	dl.progress = []quickget.DownloadEvent{{Type: "progress", Downloaded: 10, Total: 100, Percent: 10, SpeedMBps: 1.2, AvgMBps: 1.0, Message: "p1"}}
	st := &fakeStore{}
	m := newManagerWithFake(t, dl, st)

	snap, err := m.CreateDownload(newReq())
	if err != nil {
		t.Fatalf("CreateDownload error: %v", err)
	}
	dl.waitStarted(t)
	dl.allowReturn()
	waitForStatus(t, m, snap.ID, JobStatusCompleted)

	got, _ := m.Get(snap.ID)
	if got.Downloaded != 10 || got.Total != 100 || got.Percent != 10 || got.Message != "p1" {
		t.Fatalf("unexpected progress snapshot: %+v", got)
	}
	if !st.hasSavedProgress(snap.ID, 10) {
		t.Fatalf("expected progress state to be persisted for job %s", snap.ID)
	}
}

func TestManagerFailureMarksFailed(t *testing.T) {
	dl := newFakeDownloader()
	dl.err = errors.New("boom")
	st := &fakeStore{}
	m := newManagerWithFake(t, dl, st)

	snap, err := m.CreateDownload(newReq())
	if err != nil {
		t.Fatalf("CreateDownload error: %v", err)
	}
	dl.waitStarted(t)

	final := waitForStatus(t, m, snap.ID, JobStatusFailed)
	if final.Error != "boom" {
		t.Fatalf("expected failure error, got %q", final.Error)
	}
}

func TestManagerDefaultOutputPathDerivedFromURL(t *testing.T) {
	dl := newFakeDownloader()
	st := &fakeStore{}
	m := newManagerWithFake(t, dl, st)

	req := api.CreateDownloadRequest{
		URL:       "https://getsamplefiles.com/download/zip/sample-1.zip",
		Directory: ".",
	}
	snap, err := m.CreateDownload(req)
	if err != nil {
		t.Fatalf("CreateDownload error: %v", err)
	}
	dl.waitStarted(t)
	dl.allowReturn()
	waitForStatus(t, m, snap.ID, JobStatusCompleted)

	got, ok := m.Get(snap.ID)
	if !ok {
		t.Fatalf("job %s not found", snap.ID)
	}
	if filepath.Base(got.OutputPath) != "sample-1.zip" {
		t.Fatalf("expected derived output filename sample-1.zip, got %q", got.OutputPath)
	}
	if dl.options().OutputPath != "sample-1.zip" {
		t.Fatalf("expected downloader output filename sample-1.zip, got %q", dl.options().OutputPath)
	}
	if dl.options().BufferSize <= 0 {
		t.Fatalf("expected non-zero default buffer size, got %d", dl.options().BufferSize)
	}
}

func TestManagerReturnsFromRunningAfterDownloaderReturns(t *testing.T) {
	dl := newFakeDownloader()
	st := &fakeStore{}
	m := newManagerWithFake(t, dl, st)

	snap, err := m.CreateDownload(newReq())
	if err != nil {
		t.Fatalf("CreateDownload error: %v", err)
	}
	dl.waitStarted(t)
	dl.allowReturn()

	final := waitForStatus(t, m, snap.ID, JobStatusCompleted)
	if final.Status == JobStatusRunning || final.Status == JobStatusQueued {
		t.Fatalf("job must transition out of queued/running, got %s", final.Status)
	}
}

func TestManagerRepairsDirectoryLikeOutputPathFromStoredState(t *testing.T) {
	now := time.Now().UTC()
	st := &fakeStore{
		load: store.AgentState{
			Version: 1,
			Downloads: []api.DownloadSnapshot{
				{ID: "d1", URL: "https://getsamplefiles.com/download/zip/sample-1.zip", OutputPath: ".", Status: JobStatusPaused, CreatedAt: now, UpdatedAt: now},
			},
			UpdatedAt: now,
		},
	}
	dl := newFakeDownloader()
	m := newManagerWithFake(t, dl, st)

	if err := m.LoadState(); err != nil {
		t.Fatalf("LoadState error: %v", err)
	}
	if err := m.Resume("d1"); err != nil {
		t.Fatalf("Resume error: %v", err)
	}
	dl.waitStarted(t)
	dl.allowReturn()
	waitForStatus(t, m, "d1", JobStatusCompleted)

	opts := dl.options()
	if opts.OutputPath != "sample-1.zip" {
		t.Fatalf("expected repaired output path sample-1.zip, got %q", opts.OutputPath)
	}
}

func TestManagerPauseResumeCancelDelete(t *testing.T) {
	dl := newFakeDownloader()
	dl.block = true
	st := &fakeStore{}
	m := newManagerWithFake(t, dl, st)

	snap, err := m.CreateDownload(newReq())
	if err != nil {
		t.Fatalf("CreateDownload error: %v", err)
	}
	dl.waitStarted(t)

	if err := m.Pause(snap.ID); err != nil {
		t.Fatalf("Pause error: %v", err)
	}
	waitForStatus(t, m, snap.ID, JobStatusPaused)

	dl2 := newFakeDownloader()
	dl2.block = true
	m.SetDownloader(dl2)
	deadline := time.Now().Add(2 * time.Second)
	for {
		err = m.Resume(snap.ID)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Resume error: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	dl2.waitStarted(t)
	if dl2.callCount() != 1 {
		t.Fatalf("expected one resume call, got %d", dl2.callCount())
	}

	if err := m.Cancel(snap.ID); err != nil {
		t.Fatalf("Cancel error: %v", err)
	}
	waitForStatus(t, m, snap.ID, JobStatusCancelled)

	if err := m.Delete(snap.ID, false); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if _, ok := m.Get(snap.ID); ok {
		t.Fatal("expected job to be deleted")
	}
}

func TestManagerDeleteWithFilesRemovesOutputAndManifest(t *testing.T) {
	dl := newFakeDownloader()
	dl.block = true
	st := &fakeStore{}
	m := newManagerWithFake(t, dl, st)

	dir := t.TempDir()
	req := api.CreateDownloadRequest{
		URL:        "https://unit.test/file.bin",
		OutputPath: "file.bin",
		Directory:  dir,
	}
	snap, err := m.CreateDownload(req)
	if err != nil {
		t.Fatalf("CreateDownload error: %v", err)
	}
	dl.waitStarted(t)

	job, ok := m.Get(snap.ID)
	if !ok {
		t.Fatalf("job %s not found", snap.ID)
	}

	outputPath := job.OutputPath
	manifestPath := manifest.Path(outputPath)
	if err := os.WriteFile(outputPath, []byte("partial"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := m.Delete(snap.ID, true); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	if _, err := os.Stat(outputPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected output removed, err=%v", err)
	}
	if _, err := os.Stat(manifestPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected manifest removed, err=%v", err)
	}
}

func TestManagerEventsAndStateSavesOnTransitions(t *testing.T) {
	dl := newFakeDownloader()
	dl.progress = []quickget.DownloadEvent{{Type: "progress", Downloaded: 5, Total: 10, Percent: 50, Message: "half"}}
	st := &fakeStore{}
	m := newManagerWithFake(t, dl, st)

	sub, unsub := m.Events().Subscribe()
	defer unsub()

	snap, err := m.CreateDownload(newReq())
	if err != nil {
		t.Fatalf("CreateDownload error: %v", err)
	}
	dl.waitStarted(t)
	dl.allowReturn()
	waitForStatus(t, m, snap.ID, JobStatusCompleted)

	_ = waitEvent(t, sub, events.EventDownloadCreated)
	_ = waitEvent(t, sub, events.EventDownloadStarted)
	progress := waitEvent(t, sub, events.EventDownloadProgress)
	if progress.Percent != 50 {
		t.Fatalf("expected progress percent 50, got %v", progress.Percent)
	}
	_ = waitEvent(t, sub, events.EventDownloadCompleted)

	if st.saveCount() < 3 {
		t.Fatalf("expected multiple saves across transitions, got %d", st.saveCount())
	}
}

func TestManagerUsesConfiguredProgressIntervalForDownloader(t *testing.T) {
	t.Setenv(progressIntervalEnv, "100")
	dl := newFakeDownloader()
	st := &fakeStore{}
	m := newManagerWithFake(t, dl, st)

	snap, err := m.CreateDownload(newReq())
	if err != nil {
		t.Fatalf("CreateDownload error: %v", err)
	}
	dl.waitStarted(t)
	dl.allowReturn()
	waitForStatus(t, m, snap.ID, JobStatusCompleted)

	if got := dl.options().ProgressIntervalMs; got != 100 {
		t.Fatalf("expected progress interval 100ms, got %d", got)
	}
}

func TestManagerClampsProgressIntervalToSafeMinimum(t *testing.T) {
	t.Setenv(progressIntervalEnv, "10")
	dl := newFakeDownloader()
	st := &fakeStore{}
	m := newManagerWithFake(t, dl, st)

	snap, err := m.CreateDownload(newReq())
	if err != nil {
		t.Fatalf("CreateDownload error: %v", err)
	}
	dl.waitStarted(t)
	dl.allowReturn()
	waitForStatus(t, m, snap.ID, JobStatusCompleted)

	if got := dl.options().ProgressIntervalMs; got != 50 {
		t.Fatalf("expected clamped progress interval 50ms, got %d", got)
	}
}

func TestManagerLoadStateRecoversSnapshotsAndPublishesReady(t *testing.T) {
	now := time.Now().UTC()
	completedAt := now.Add(-time.Minute)
	st := &fakeStore{
		load: store.AgentState{
			Version: 1,
			Downloads: []api.DownloadSnapshot{
				{ID: "c1", URL: "https://unit.test/c", OutputPath: "c.bin", Status: JobStatusCompleted, CreatedAt: now, UpdatedAt: now, CompletedAt: &completedAt},
				{ID: "f1", URL: "https://unit.test/f", OutputPath: "f.bin", Status: JobStatusFailed, Error: "boom", CreatedAt: now, UpdatedAt: now},
				{ID: "p1", URL: "https://unit.test/p", OutputPath: "p.bin", Status: JobStatusPaused, Message: "paused before", CreatedAt: now, UpdatedAt: now},
				{ID: "r1", URL: "https://unit.test/r", OutputPath: "r.bin", Status: JobStatusRunning, Message: "was running", CreatedAt: now, UpdatedAt: now},
			},
			UpdatedAt: now,
		},
	}
	m := NewManager(st)
	sub, unsub := m.Events().Subscribe()
	defer unsub()

	if err := m.LoadState(); err != nil {
		t.Fatalf("LoadState error: %v", err)
	}

	c1, ok := m.Get("c1")
	if !ok || c1.Status != JobStatusCompleted {
		t.Fatalf("completed job not preserved: %+v ok=%v", c1, ok)
	}
	f1, ok := m.Get("f1")
	if !ok || f1.Status != JobStatusFailed || f1.Error != "boom" {
		t.Fatalf("failed job not preserved: %+v ok=%v", f1, ok)
	}
	p1, ok := m.Get("p1")
	if !ok || p1.Status != JobStatusPaused || p1.Message != "paused before" {
		t.Fatalf("paused job not preserved: %+v ok=%v", p1, ok)
	}
	r1, ok := m.Get("r1")
	if !ok || r1.Status != JobStatusPaused {
		t.Fatalf("running job not normalized to paused: %+v ok=%v", r1, ok)
	}
	if r1.Message != "Agent stopped while this download was running. Resume to continue." {
		t.Fatalf("unexpected normalized message: %q", r1.Message)
	}
	if st.saveCount() != 1 {
		t.Fatalf("expected normalized state save, got %d", st.saveCount())
	}

	ev := waitEvent(t, sub, events.EventAgentReady)
	if ev.Status != "ready" {
		t.Fatalf("expected ready event status, got %q", ev.Status)
	}
}
