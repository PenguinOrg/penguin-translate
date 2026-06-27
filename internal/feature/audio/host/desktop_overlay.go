package host

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"translation-overlay/internal/platform/osproc"
	"translation-overlay/internal/platform/overlaybinary"
)

type desktopOverlayProc struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	running bool
	lastErr string

	vrSeen   bool
	vrOK     bool
	vrDetail string

	load   func() settingsFile
	logDir string
}

var desktopOverlay desktopOverlayProc

func (o *desktopOverlayProc) settings() settingsFile {
	if o.load != nil {
		return o.load()
	}
	return defaultSettingsFile()
}

type overlayStatusWriter struct {
	o   *desktopOverlayProc
	buf []byte
}

func (w *overlayStatusWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSpace(string(w.buf[:i]))
		w.buf = w.buf[i+1:]
		if line != "" {
			w.o.handleStdoutLine(line)
		}
	}
	return len(p), nil
}

func (o *desktopOverlayProc) handleStdoutLine(line string) {
	var head struct {
		Event string `json:"event"`
	}
	if json.Unmarshal([]byte(line), &head) == nil {
		switch head.Event {
		case "vr_status":
			var ev struct {
				OK     bool   `json:"ok"`
				Detail string `json:"detail"`
			}
			_ = json.Unmarshal([]byte(line), &ev)
			o.mu.Lock()
			o.vrSeen = true
			o.vrOK = ev.OK
			o.vrDetail = ev.Detail
			o.mu.Unlock()
			writeOverlayLog("vr", line)
			return
		case "caption_timing":
			var t struct {
				ReqID   uint64           `json:"req_id"`
				SpansUS map[string]int64 `json:"spans_us"`
				TotalUS int64            `json:"total_us"`
			}
			if json.Unmarshal([]byte(line), &t) == nil {
				overlayTimings.recordRust(t.ReqID, stageSpans(t.SpansUS), t.TotalUS)
			}
			writeOverlayLog("timing", line)
			return
		}
	}
	log.Printf("overlay: %s", line)
	writeOverlayLog("stdout", line)
}

func (o *desktopOverlayProc) isRunning() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cmd != nil && o.cmd.Process != nil && o.running
}

func (o *desktopOverlayProc) vrStatus() (seen, ok bool, detail string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.vrSeen, o.vrOK, o.vrDetail
}

func overlayDesktopExePath() string {
	if p := strings.TrimSpace(os.Getenv("TO_OVERLAY_EXE")); p != "" {
		return p
	}
	if p, err := overlaybinary.Materialize(); err == nil {
		return p
	}
	exe, err := os.Executable()
	if err != nil {
		return "penguin-translate-overlay.exe"
	}
	sibling := filepath.Join(filepath.Dir(exe), "penguin-translate-overlay.exe")
	if _, err := os.Stat(sibling); err == nil {
		return sibling
	}
	return "penguin-translate-overlay.exe"
}

func (o *desktopOverlayProc) status() map[string]any {
	o.mu.Lock()
	defer o.mu.Unlock()
	alive := o.cmd != nil && o.cmd.Process != nil && o.running
	return map[string]any{
		"enabled":     o.running,
		"initialized": alive,
		"running":     alive,
		"supported":   runtime.GOOS == "windows",
		"last_error":  o.lastErr,
		"exe":         overlayDesktopExePath(),
	}
}

func (o *desktopOverlayProc) writeOpLocked(v any) error {
	if o.stdin == nil {
		return fmt.Errorf("overlay process not running")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = o.stdin.Write(append(b, '\n'))
	if err != nil {
		o.lastErr = err.Error()
	}
	return err
}

func (o *desktopOverlayProc) writeOp(v any) error {
	done := make(chan error, 1)
	go func() {
		o.mu.Lock()
		err := o.writeOpLocked(v)
		o.mu.Unlock()
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		o.mu.Lock()
		o.lastErr = "overlay stdin write timed out"
		o.mu.Unlock()
		return fmt.Errorf("overlay stdin write timed out")
	}
}

func (o *desktopOverlayProc) start() (string, error) {
	if runtime.GOOS != "windows" {
		o.mu.Lock()
		o.lastErr = "desktop overlay is Windows-only"
		o.mu.Unlock()
		return "", fmt.Errorf("%s", o.lastErr)
	}
	o.mu.Lock()
	if o.running && o.stdin != nil {
		o.mu.Unlock()
		applyOverlayConfigure(o.settings())
		return "already running", nil
	}
	o.mu.Unlock()

	exe := overlayDesktopExePath()
	if _, err := os.Stat(exe); err != nil {
		o.mu.Lock()
		o.lastErr = fmt.Sprintf("overlay binary missing: %s (rebuild with build/scripts/build.ps1)", exe)
		o.mu.Unlock()
		log.Printf("desktop overlay: %s", o.lastErr)
		return "", fmt.Errorf("%s", o.lastErr)
	}

	cmd := exec.Command(exe)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	cmd.Stdout = &overlayStatusWriter{o: o}
	cmd.Stderr = &overlayStderrWriter{}
	osproc.Hide(cmd)
	if err := cmd.Start(); err != nil {
		return "", err
	}

	o.mu.Lock()
	o.cmd = cmd
	o.stdin = stdin
	o.running = true
	o.lastErr = ""
	o.vrSeen = false
	o.vrOK = false
	o.vrDetail = ""
	o.mu.Unlock()

	go func() {
		err := cmd.Wait()
		o.mu.Lock()
		o.running = false
		o.stdin = nil
		o.cmd = nil
		if err != nil {
			o.lastErr = err.Error()
		}
		o.mu.Unlock()
		log.Printf("desktop overlay process exited: %v", err)
	}()

	log.Printf("desktop overlay started pid=%d exe=%s", cmd.Process.Pid, exe)
	applyOverlayConfigure(o.settings())
	return "started", nil
}

func (o *desktopOverlayProc) stop() {
	o.mu.Lock()
	if o.stdin != nil {
		_ = o.writeOpLocked(map[string]string{"op": "quit"})
		_ = o.stdin.Close()
	}
	if o.cmd != nil && o.cmd.Process != nil {
		_ = o.cmd.Process.Kill()
	}
	o.stdin = nil
	o.cmd = nil
	o.running = false
	o.mu.Unlock()
}

func (o *desktopOverlayProc) ensureStarted() {
	s := o.settings()
	if !s.DesktopOverlayEnabled && !s.OpenVROverlayEnabled {
		return
	}
	if desktopOverlay.isRunning() {
		applyOverlayConfigure(s)
		return
	}
	if _, err := desktopOverlay.start(); err != nil {
		log.Printf("overlay ensureStarted: %v", err)
	}
}

func (o *desktopOverlayProc) setCaption(reqID uint64, reading, source, english string, spans stageSpans) {
	o.ensureStarted()
	tw := time.Now()
	err := o.writeOp(map[string]any{
		"op":           "caption",
		"req_id":       reqID,
		"line_reading": reading,
		"line_source":  source,
		"line_english": english,
	})
	if spans == nil {
		spans = stageSpans{}
	}
	spans["ipc_write"] = usSince(tw)
	if err != nil {
		log.Printf("desktop overlay setCaption: %v", err)
	}
	total := spans.total()
	overlayTimings.recordGo(reqID, spans, total)
	logCaptionTiming("go", reqID, spans, total)
}

func handleDesktopOverlayStatus(w http.ResponseWriter, r *http.Request) {
	if !isGetOrHead(r) {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(desktopOverlay.status())
}

func handleDesktopOverlayStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	msg, err := desktopOverlay.start()
	out := map[string]string{"status": msg}
	if err != nil {
		out["warning"] = err.Error()
	} else if st := desktopOverlay.status(); st["last_error"] != nil {
		if s, ok := st["last_error"].(string); ok && s != "" {
			out["warning"] = s
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func handleDesktopOverlayStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	desktopOverlay.stop()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func handleDesktopOverlayClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := desktopOverlay.writeOp(map[string]string{"op": "hide"}); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
}

func pushOverlayCaptionFromTranscribeJSON(body []byte, wantTranslate bool, goSpans stageSpans) {
	s := desktopOverlay.settings()
	if !s.DesktopOverlayEnabled && !s.OpenVROverlayEnabled {
		return
	}
	var resp struct {
		Segments []overlaySegment `json:"segments"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Segments) == 0 {
		return
	}
	reading, source, english := captionLinesFromSegment(resp.Segments[len(resp.Segments)-1], wantTranslate)
	if reading == "" && source == "" && english == "" {
		return
	}
	desktopOverlay.setCaption(nextReqID(), reading, source, english, goSpans)
}

func applyOverlayConfigure(s settingsFile) {
	s = normalizeSettings(s)
	_ = desktopOverlay.writeOp(map[string]any{
		"op":              "configure",
		"width":           s.DesktopOverlayWidth,
		"font_scale":      s.DesktopOverlayFontScale,
		"align":           s.DesktopOverlayAlign,
		"margin_bottom":   s.DesktopOverlayMarginBottom,
		"margin_x":        s.DesktopOverlayMarginX,
		"desktop_enabled": s.DesktopOverlayEnabled,
		"vr_enabled":      s.OpenVROverlayEnabled,
		"vr_width_m":      s.VROverlayWidthM,
		"vr_distance_m":   s.VROverlayDistanceM,
		"vr_y_offset_m":   s.VROverlayYOffsetM,
	})
}

func syncOverlayLayoutFromSettings(s settingsFile) {
	s = normalizeSettings(s)
	if !s.DesktopOverlayEnabled && !s.OpenVROverlayEnabled {
		if desktopOverlay.isRunning() {
			applyOverlayConfigure(s)
		}
		return
	}
	desktopOverlay.ensureStarted()
	applyOverlayConfigure(s)
}
