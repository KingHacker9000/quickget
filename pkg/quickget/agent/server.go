package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"quickget/pkg/quickget/api"
)

const (
	DefaultAgentAddr = "127.0.0.1:19329"
	defaultVersion   = "dev"
)

type Server struct {
	manager *Manager
	token   string
	version string
	httpSrv *http.Server
}

func NewServer(manager *Manager, token string, version string) *Server {
	if version == "" {
		version = defaultVersion
	}

	s := &Server{
		manager: manager,
		token:   token,
		version: version,
	}

	s.httpSrv = &http.Server{
		Addr:    DefaultAgentAddr,
		Handler: s,
	}

	return s
}

func (s *Server) SetAddr(addr string) {
	if strings.TrimSpace(addr) == "" {
		s.httpSrv.Addr = DefaultAgentAddr
		return
	}
	s.httpSrv.Addr = addr
}

func (s *Server) Addr() string {
	return s.httpSrv.Addr
}

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.httpSrv.Addr)
	if err != nil {
		return err
	}

	if tcpAddr, ok := ln.Addr().(*net.TCPAddr); !ok || !tcpAddr.IP.IsLoopback() {
		_ = ln.Close()
		return fmt.Errorf("agent server must bind to loopback, got %q", ln.Addr().String())
	}

	err = s.httpSrv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/health" {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		s.handleHealth(w)
		return
	}

	if !CheckBearerToken(r.Header.Get("Authorization"), s.token) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token")
		return
	}

	if r.URL.Path == "/downloads" {
		s.handleDownloadsCollection(w, r)
		return
	}

	if r.URL.Path == "/events" {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		s.handleEvents(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/downloads/") {
		s.handleDownloadsItem(w, r)
		return
	}

	writeError(w, http.StatusNotFound, "not_found", "route not found")
}

func (s *Server) handleHealth(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"name":    "quickget-agent",
		"version": s.version,
	})
}

func (s *Server) handleDownloadsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.manager.List())
	case http.MethodPost:
		req, ok := decodeJSONBody[api.CreateDownloadRequest](w, r)
		if !ok {
			return
		}
		snap, err := s.manager.CreateDownload(req)
		if err != nil {
			writeManagerError(w, err)
			return
		}
		if created, found := s.manager.Get(snap.ID); found {
			snap = created
		}
		writeJSON(w, http.StatusCreated, snap)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleDownloadsItem(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/downloads/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}

	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}
	id := parts[0]

	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		snap, ok := s.manager.Get(id)
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "download not found")
			return
		}
		writeJSON(w, http.StatusOK, snap)
		return
	}

	if len(parts) != 2 {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}

	action := parts[1]
	switch action {
	case "pause":
		s.handleJobAction(w, r, id, s.manager.Pause)
	case "resume":
		s.handleJobAction(w, r, id, s.manager.Resume)
	case "cancel":
		s.handleJobAction(w, r, id, s.manager.Cancel)
	case "delete":
		s.handleDelete(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
	}
}

func (s *Server) handleJobAction(w http.ResponseWriter, r *http.Request, id string, action func(string) error) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if err := action(id); err != nil {
		writeManagerError(w, err)
		return
	}
	snap, ok := s.manager.Get(id)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	type deleteRequest struct {
		DeleteFiles bool `json:"delete_files"`
	}

	body := deleteRequest{}
	if r.Body != nil && r.ContentLength != 0 {
		parsed, ok := decodeJSONBody[deleteRequest](w, r)
		if !ok {
			return
		}
		body = parsed
	}

	if err := s.manager.Delete(id, body.DeleteFiles); err != nil {
		writeManagerError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "delete_files": body.DeleteFiles})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "unsupported", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	eventsCh, unsubscribe := s.manager.Events().Subscribe()
	defer unsubscribe()

	_, _ = w.Write([]byte(": connected\n\n"))
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-eventsCh:
			if !ok {
				return
			}

			payload, err := json.Marshal(ev)
			if err != nil {
				continue
			}

			_, err = fmt.Fprintf(w, "event: %s\n", ev.Type)
			if err != nil {
				return
			}
			_, err = fmt.Fprintf(w, "data: %s\n\n", payload)
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeManagerError(w http.ResponseWriter, err error) {
	msg := err.Error()
	if strings.Contains(msg, "job not found") {
		writeError(w, http.StatusNotFound, "not_found", msg)
		return
	}
	if strings.Contains(msg, "required") || strings.Contains(msg, "invalid") {
		writeError(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	if strings.Contains(msg, "already running") || strings.Contains(msg, "cannot be") || strings.Contains(msg, "not running") {
		writeError(w, http.StatusConflict, "invalid_state", msg)
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func writeMethodNotAllowed(w http.ResponseWriter, methods ...string) {
	if len(methods) > 0 {
		w.Header().Set("Allow", strings.Join(methods, ", "))
	}
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func decodeJSONBody[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var zero T
	if ct := strings.TrimSpace(r.Header.Get("Content-Type")); ct != "" {
		mimeType := strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
		if mimeType != "application/json" {
			writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "content-type must be application/json")
			return zero, false
		}
	}

	defer r.Body.Close()

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var payload T
	if err := dec.Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return zero, false
	}

	if dec.More() {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return zero, false
	}

	return payload, true
}

func (s *Server) SetReadTimeout(d time.Duration) {
	s.httpSrv.ReadTimeout = d
}

func (s *Server) SetWriteTimeout(d time.Duration) {
	s.httpSrv.WriteTimeout = d
}
