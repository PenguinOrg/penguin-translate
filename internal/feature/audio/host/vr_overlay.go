package host

import (
	"encoding/json"
	"net/http"
)

func (h *Host) handleVROverlayStatus(w http.ResponseWriter, r *http.Request) {
	if !isGetOrHead(r) {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	seen, ok, detail := desktopOverlay.vrStatus()
	running := desktopOverlay.isRunning() && h.readSettingsFromDisk().OpenVROverlayEnabled
	lastErr := ""
	if seen && !ok {
		lastErr = detail
	}
	if detail == "" {
		detail = "starting"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"running":     running,
		"initialized": ok,
		"has_steamvr": ok,
		"detail":      detail,
		"last_error":  lastErr,
	})
}

func (h *Host) handleVROverlayStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	desktopOverlay.ensureStarted()
	applyOverlayConfigure(h.readSettingsFromDisk())
	out := map[string]string{"status": "started"}
	if seen, ok, detail := desktopOverlay.vrStatus(); seen && !ok {
		out["warning"] = detail
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func handleVROverlayStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = desktopOverlay.writeOp(map[string]any{"op": "configure", "vr_enabled": false})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func handleVROverlayClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = desktopOverlay.writeOp(map[string]any{"op": "hide"})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
}
