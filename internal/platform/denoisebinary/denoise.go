package denoisebinary

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const fileName = "penguin-translate-denoise.exe"

var ErrNotEmbedded = errors.New("denoise sidecar binary is not embedded in this build")

func appDataDir() (string, error) {
	if d := strings.TrimSpace(os.Getenv("TO_DATA_DIR")); d != "" {
		return d, nil
	}
	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return "", errors.New("LOCALAPPDATA is not set")
		}
		return filepath.Join(local, "translation-overlay"), nil
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "translation-overlay"), nil
	}
}

func Materialize() (string, error) {
	if !embedded() {
		return "", ErrNotEmbedded
	}
	raw, err := embeddedBytes()
	if err != nil {
		return "", err
	}
	dataDir, err := appDataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(dataDir, "bin")
	target := filepath.Join(dir, fileName)
	sum := sha256.Sum256(raw)
	want := hex.EncodeToString(sum[:]) + "\n"
	verPath := filepath.Join(dir, ".denoise_ver")
	cur, _ := os.ReadFile(verPath)
	if string(cur) == want {
		if _, err := os.Stat(target); err == nil {
			return target, nil
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, raw, 0o755); err != nil {
		return "", fmt.Errorf("write denoise: %w", err)
	}
	if err := os.WriteFile(verPath, []byte(want), 0o644); err != nil {
		return "", err
	}
	return target, nil
}
