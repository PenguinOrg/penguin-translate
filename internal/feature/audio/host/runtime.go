package host

import (
	"encoding/json"
	"net/http"
	"runtime"

	"translation-overlay/internal/platform/audio"
	"translation-overlay/internal/platform/engine"
)

func (h *Host) handleAudioRuntime(w http.ResponseWriter, r *http.Request) {
	if !isGetOrHead(r) {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	managedEngine := "skipped"
	if !engine.ManagedEngineSkipped() {
		managedEngine = "python"
	}

	overlayVR := "unavailable"
	if runtime.GOOS == "windows" {
		overlayVR = "go"
	}

	out := map[string]any{
		"native_audio":     true,
		"loopback_ws_url":  audio.NativeLoopbackWSURL(),
		"loopback_http":    audio.NativeLoopbackBaseURL(),
		"transcribe":       "go",
		"managed_engine":   managedEngine,
		"inference_engine": engine.EngineURL(),
		"loopback_capture": "go-wca",
		"overlay_vr":       overlayVR,
		"overlay_desktop":  "rust-sidecar",
	}
	if r.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}
