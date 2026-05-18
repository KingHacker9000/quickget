package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"quickget/pkg/quickget/api"
)

type AgentState struct {
	Version   int                    `json:"version"`
	Downloads []api.DownloadSnapshot `json:"downloads"`
	Captures  []api.BrowserCapture   `json:"captures,omitempty"`
	UpdatedAt time.Time              `json:"updatedAt"`
}

type JSONStore struct {
	Path string
}

func DefaultStatePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(configDir, "QuickGet", "agent-state.json"), nil
}

func (s JSONStore) Load() (AgentState, error) {
	var state AgentState

	if s.Path == "" {
		return state, errors.New("store path is required")
	}

	b, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		return state, err
	}

	if len(b) == 0 {
		return state, nil
	}

	if err := json.Unmarshal(b, &state); err != nil {
		return AgentState{}, err
	}

	return state, nil
}

func (s JSONStore) Save(state AgentState) error {
	if s.Path == "" {
		return errors.New("store path is required")
	}

	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}

	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(s.Path), filepath.Base(s.Path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	cleanup := func() {
		_ = os.Remove(tmpPath)
	}

	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}

	if err := os.Rename(tmpPath, s.Path); err != nil {
		cleanup()
		return err
	}

	return nil
}
