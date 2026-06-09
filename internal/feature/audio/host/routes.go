package host

import (
	"encoding/json"
	"net/http"

	"translation-overlay/internal/platform/cloudapi"
)

func isGetOrHead(r *http.Request) bool {
	return r.Method == http.MethodGet || r.Method == http.MethodHead
}

func (h *Host) MountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/transcribe-segment", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleNativeTranscribeSegment(w, r)
	})
	mux.HandleFunc("/api/audio/runtime", h.handleAudioRuntime)
	mux.HandleFunc("/api/audio/apply-overlay-layout", h.handleApplyOverlayLayout)
	mux.HandleFunc("/api/loopback", handleNativeLoopbackWS)
	mux.HandleFunc("/api/caption-presets", func(w http.ResponseWriter, r *http.Request) {
		if !isGetOrHead(r) {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cloudapi.CaptionPresets())
	})
	mux.HandleFunc("/api/overlay/status", h.handleVROverlayStatus)
	mux.HandleFunc("/api/overlay/start", h.handleVROverlayStart)
	mux.HandleFunc("/api/overlay/stop", handleVROverlayStop)
	mux.HandleFunc("/api/overlay/clear", handleVROverlayClear)
	mux.HandleFunc("/api/desktop-overlay/status", handleDesktopOverlayStatus)
	mux.HandleFunc("/api/desktop-overlay/start", handleDesktopOverlayStart)
	mux.HandleFunc("/api/desktop-overlay/stop", handleDesktopOverlayStop)
	mux.HandleFunc("/api/desktop-overlay/clear", handleDesktopOverlayClear)
	mux.HandleFunc("/api/overlay/timings", handleOverlayTimings)
	mux.HandleFunc("/api/overlay/timings/stream", handleOverlayTimingsStream)
	mux.HandleFunc("/api/loopback/devices", func(w http.ResponseWriter, r *http.Request) {
		if !isGetOrHead(r) {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleNativeLoopbackDevices(w, r)
	})

	mux.HandleFunc("/api/loopback/health", func(w http.ResponseWriter, r *http.Request) {
		if !isGetOrHead(r) {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleNativeLoopbackHealth(w, r)
	})
}
