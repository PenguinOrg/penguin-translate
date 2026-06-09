//go:build windows

package ocr

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var ocrFiles = []string{"oneocr.dll", "oneocr.onemodel", "onnxruntime.dll"}

func ResolveDir(configured string) (string, error) {
	if configured != "" {
		if ok, err := hasOneOCR(configured); err != nil {
			return "", err
		} else if ok {
			return configured, nil
		}
	}
	local, err := localOCRDir()
	if err != nil {
		return "", err
	}
	if ok, _ := hasOneOCR(local); ok {
		return local, nil
	}
	src, err := DiscoverDir("")
	if err != nil {
		return "", err
	}
	if err := copyOCRBundle(src, local); err != nil {
		return "", fmt.Errorf("copy ocr bundle from %s: %w", src, err)
	}
	return local, nil
}

func localOCRDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "window-translate", "ocr"), nil
}

func copyOCRBundle(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, name := range ocrFiles {
		from := filepath.Join(src, name)
		to := filepath.Join(dst, name)
		if err := copyFile(from, to); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
