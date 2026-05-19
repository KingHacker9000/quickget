package nativehost

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	ChromeHostName = "com.quickget.download_manager"
)

type InstallResult struct {
	ManifestPath       string
	RegistryConfigured bool
}

func InstallChrome(hostExecutablePath string, allowedOrigins []string) (InstallResult, error) {
	if runtime.GOOS != "windows" {
		return InstallResult{}, errors.New("install-chrome is currently supported on Windows only")
	}
	exe := strings.TrimSpace(hostExecutablePath)
	if exe == "" {
		return InstallResult{}, errors.New("host executable path is required")
	}
	exe, err := filepath.Abs(exe)
	if err != nil {
		return InstallResult{}, err
	}
	exe = normalizeWindowsExecutablePath(exe)
	origins, err := normalizeAllowedOrigins(allowedOrigins)
	if err != nil {
		return InstallResult{}, err
	}

	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return InstallResult{}, err
	}
	manifestDir := filepath.Join(cfgDir, "QuickGet", "native-host")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return InstallResult{}, err
	}
	manifestPath := filepath.Join(manifestDir, ChromeHostName+".json")
	manifest := map[string]any{
		"name":            ChromeHostName,
		"description":     "QuickGet native messaging host",
		"path":            exe,
		"type":            "stdio",
		"allowed_origins": origins,
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return InstallResult{}, err
	}
	b = append(b, '\n')
	if err := os.WriteFile(manifestPath, b, 0o644); err != nil {
		return InstallResult{}, err
	}

	regKey := `HKCU\Software\Google\Chrome\NativeMessagingHosts\` + ChromeHostName
	cmd := exec.Command("reg", "add", regKey, "/ve", "/t", "REG_SZ", "/d", manifestPath, "/f")
	if err := cmd.Run(); err != nil {
		return InstallResult{ManifestPath: manifestPath, RegistryConfigured: false}, nil
	}

	return InstallResult{ManifestPath: manifestPath, RegistryConfigured: true}, nil
}

func UninstallChrome() error {
	if runtime.GOOS != "windows" {
		return errors.New("uninstall-chrome is currently supported on Windows only")
	}
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(cfgDir, "QuickGet", "native-host", ChromeHostName+".json")
	_ = os.Remove(manifestPath)
	regKey := `HKCU\Software\Google\Chrome\NativeMessagingHosts\` + ChromeHostName
	_ = exec.Command("reg", "delete", regKey, "/f").Run()
	return nil
}

func InstallHelp(exePath string) string {
	return fmt.Sprintf("If registry registration failed, create key HKCU\\\\Software\\\\Google\\\\Chrome\\\\NativeMessagingHosts\\\\%s with default value set to manifest path. Executable: %s", ChromeHostName, exePath)
}

func normalizeAllowedOrigins(origins []string) ([]string, error) {
	seen := make(map[string]struct{}, len(origins))
	out := make([]string, 0, len(origins))
	for _, raw := range origins {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if !strings.HasPrefix(v, "chrome-extension://") || !strings.HasSuffix(v, "/") {
			return nil, fmt.Errorf("invalid allowed origin %q; expected format chrome-extension://<extension-id>/", v)
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, errors.New("at least one extension origin is required; pass -origin chrome-extension://<extension-id>/")
	}
	return out, nil
}

func normalizeWindowsExecutablePath(path string) string {
	const (
		devicePrefix = `\\?\`
		uncPrefix    = `\\?\UNC\`
	)
	if strings.HasPrefix(path, uncPrefix) {
		return `\\` + strings.TrimPrefix(path, uncPrefix)
	}
	if strings.HasPrefix(path, devicePrefix) {
		return strings.TrimPrefix(path, devicePrefix)
	}
	return path
}
