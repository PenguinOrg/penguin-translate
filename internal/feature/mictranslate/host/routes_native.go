package host

import (
	"net/http"

	"translation-overlay/internal/platform/engine"
)

func (h *Host) handleSpeakTTS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if useNativeCloud() {
		h.handleNativeSpeakTTS(w, r)
		return
	}
	h.proxySpeakTTS(w, r)
}

func (h *Host) handlePlayWav(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if useNativeCloud() {
		handleNativePlayWAV(w, r)
		return
	}
	h.proxyPlayWav(w, r)
}

func handleEngineHealth(w http.ResponseWriter, r *http.Request) {
	if !isGetOrHead(r) {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if engine.ManagedEngineSkipped() {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"cloud-native","device_detail":"Python engine not loaded","audio":"go-native","loopback_port":"8746"}`))
		return
	}
	proxyGET(w, "/health")
}
