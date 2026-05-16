package agent

import (
	"os"
	"strconv"
	"strings"

	"quickget/pkg/quickget/progress"
)

const (
	progressIntervalEnv            = "QDM_AGENT_PROGRESS_INTERVAL_MS"
	debugProgressEnv               = "QDM_DEBUG_PROGRESS"
	defaultAgentProgressIntervalMs = 100
	progressPersistIntervalMs      = 1000
)

func readAgentProgressIntervalMs() int {
	raw := strings.TrimSpace(os.Getenv(progressIntervalEnv))
	if raw == "" {
		return defaultAgentProgressIntervalMs
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return defaultAgentProgressIntervalMs
	}
	if v < progress.MinRefreshIntervalMS {
		return progress.MinRefreshIntervalMS
	}
	return v
}

func debugProgressEnabled() bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(debugProgressEnv)))
	return raw == "1" || raw == "true" || raw == "yes"
}
