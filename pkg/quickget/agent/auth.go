package agent

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const tokenFileName = "agent-token"

// LoadOrCreateToken loads the local agent auth token from config dir, or creates it.
func LoadOrCreateToken() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	dir := filepath.Join(configDir, "QuickGet")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}

	path := filepath.Join(dir, tokenFileName)
	if data, err := os.ReadFile(path); err == nil {
		token := strings.TrimSpace(string(data))
		if token == "" {
			return "", errors.New("agent token file is empty")
		}
		return token, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	token, err := generateToken()
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}

	return token, nil
}

// CheckBearerToken validates an Authorization header value against expected token.
func CheckBearerToken(headerValue string, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return false
	}

	headerValue = strings.TrimSpace(headerValue)
	const prefix = "Bearer "
	if len(headerValue) <= len(prefix) || !strings.EqualFold(headerValue[:len(prefix)], prefix) {
		return false
	}

	provided := strings.TrimSpace(headerValue[len(prefix):])
	if provided == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
