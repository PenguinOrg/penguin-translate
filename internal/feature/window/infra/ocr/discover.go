//go:build windows

package ocr

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"translation-overlay/internal/platform/osproc"
)

func DiscoverDir(configured string) (string, error) {
	if configured != "" {
		if ok, err := hasOneOCR(configured); err != nil {
			return "", err
		} else if ok {
			return configured, nil
		}
		return "", fmt.Errorf("ocr_dir %q missing oneocr.dll", configured)
	}
	if env := os.Getenv("ONEOCR_DIR"); env != "" {
		if ok, _ := hasOneOCR(env); ok {
			return env, nil
		}
	}
	if p := snippingToolDir(); p != "" {
		if ok, _ := hasOneOCR(p); ok {
			return p, nil
		}
	}
	return "", fmt.Errorf("oneocr.dll not found: install Microsoft Snipping Tool (ScreenSketch) or set ocr_dir in config")
}

func hasOneOCR(dir string) (bool, error) {
	_, err := os.Stat(filepath.Join(dir, "oneocr.dll"))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func snippingToolDir() string {
	cmd := exec.Command(
		"powershell", "-NoProfile", "-NonInteractive", "-Command",
		`(Get-AppxPackage Microsoft.ScreenSketch -ErrorAction SilentlyContinue).InstallLocation + '\SnippingTool'`,
	)
	osproc.Hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(out))
	if p == "" || p == `\SnippingTool` {
		return ""
	}
	return p
}
