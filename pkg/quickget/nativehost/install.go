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

func InstallChrome(hostExecutablePath string) (InstallResult, error) {
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
		"allowed_origins": []string{},
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
