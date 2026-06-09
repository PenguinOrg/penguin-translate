package host

import (
	"bytes"
	"io"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"translation-overlay/internal/platform/applog"
)

var (
	overlayLogMu sync.Mutex
	overlayLog   *applog.RotatingWriter
)

func ensureOverlayLog() *applog.RotatingWriter {
	overlayLogMu.Lock()
	defer overlayLogMu.Unlock()
	if overlayLog != nil {
		return overlayLog
	}
	dir := desktopOverlay.logDir
	if strings.TrimSpace(dir) == "" || dir == "." {
		return nil
	}
	w, err := applog.NewRotatingWriter(filepath.Join(dir, "overlay.log"), 2<<20, 3)
	if err != nil {
		log.Printf("overlay log: %v", err)
		return nil
	}
	overlayLog = w
	return overlayLog
}

func writeOverlayLog(tag, line string) {
	w := ensureOverlayLog()
	if w == nil {
		return
	}
	_, _ = io.WriteString(w, time.Now().Format("2006-01-02T15:04:05.000")+" ["+tag+"] "+line+"\n")
}

type overlayStderrWriter struct {
	buf []byte
}

func (w *overlayStderrWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf[:i]), "\r")
		w.buf = w.buf[i+1:]
		if strings.TrimSpace(line) != "" {
			writeOverlayLog("stderr", line)
		}
	}
	return len(p), nil
}
