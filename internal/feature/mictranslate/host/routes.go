package host

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"translation-overlay/internal/platform/engine"
)

func isGetOrHead(r *http.Request) bool {
	return r.Method == http.MethodGet || r.Method == http.MethodHead
}

func (h *Host) MountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if !isGetOrHead(r) {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ui":"ok","engine":%q}`, engineURL())
	})

	mux.HandleFunc("/api/engine-health", handleEngineHealth)

	mux.HandleFunc("/api/engine-load", h.handleEngineLoad)

	mux.HandleFunc("/api/pipeline", h.handlePipeline)
	mux.HandleFunc("/api/translate-text", h.handleTranslateText)
	mux.HandleFunc("/api/transcribe", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if useNativeCloud() {
			h.handleNativeTranscribe(w, r)
			return
		}
		h.forwardMultipart(w, r, "/transcribe")
	})

	mux.HandleFunc("/api/score", h.handleScore)
	mux.HandleFunc("/api/score-ja", h.handleScore)
	mux.HandleFunc("/api/play-wav", h.handlePlayWav)
	mux.HandleFunc("/api/speak-tts", h.handleSpeakTTS)

	mux.HandleFunc("/api/devices/output", func(w http.ResponseWriter, r *http.Request) {
		if !isGetOrHead(r) {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if useNativeCloud() {
			handleNativeDevicesOutput(w, r)
			return
		}
		proxyGET(w, "/devices/output")
	})

	mux.HandleFunc("/api/cuda-devices", handleCudaDevices)
	mux.HandleFunc("/api/languages", handleLanguages)
	mux.HandleFunc("/api/plugins", handlePluginsList)
	mux.HandleFunc("/api/plugins/vrchat-osc/send", handleVRChatOscSend)
	mux.HandleFunc("/api/debug/logs", h.handleDebugLogs)
	mux.HandleFunc("/api/launcher-status", engine.HandleLauncherStatus)
	mux.HandleFunc("/api/health-summary", h.handleHealthSummary)
}

type healthSummaryPart struct {
	State  string `json:"state"`
	Detail string `json:"detail"`
}

type healthSummaryJSON struct {
	Engine   healthSummaryPart     `json:"engine"`
	Models   healthSummaryPart     `json:"models"`
	OpenAI   healthSummaryPart     `json:"openai"`
	Launcher engine.LauncherStatus `json:"launcher"`
}

func (h *Host) handleHealthSummary(w http.ResponseWriter, r *http.Request) {
	if !isGetOrHead(r) {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}

	out := healthSummaryJSON{Launcher: engine.Launcher().Snapshot()}

	engineState := "pending"
	engineDetail := ""
	switch out.Launcher.Phase {
	case engine.PhaseFailed:
		engineState = "error"
		engineDetail = out.Launcher.Error
	case engine.PhaseReady:
		if engine.ManagedEngineSkipped() {
			engineState = "ok"
			engineDetail = "Cloud-native (Python engine not loaded)"
		} else {
			client := &http.Client{Timeout: 1 * time.Second}
			req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, engineURL()+"/health", nil)
			if err == nil {
				resp, err := client.Do(req)
				if err == nil {
					resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						engineState = "ok"
						engineDetail = engineURL()
					} else {
						engineState = "error"
						engineDetail = "engine returned " + resp.Status
					}
				} else {
					engineState = "error"
					engineDetail = err.Error()
				}
			}
		}
	default:
		engineState = "pending"
		engineDetail = out.Launcher.Label
	}
	out.Engine = healthSummaryPart{State: engineState, Detail: engineDetail}

	modelsState := "pending"
	modelsDetail := "Not loaded — use Load models in Mic translate settings"
	s := h.readSettingsFromDisk()
	localNeed := needsLocalModels(s)
	if engineState == "ok" {
		if !localNeed.Any() {
			modelsState = "ok"
			modelsDetail = "Cloud pipeline — local Whisper/NLLB not required"
		} else {
			go engine.TryFetchLoadSnapshot(r.Context())
			if snap := engine.LastLoadSnapshot(); snap.Status == "ready" {
				modelsState = "ok"
				modelsDetail = snap.Detail
			} else {
				modelsState = "pending"
				modelsDetail = "Not loaded — click Load models in Mic translate settings"
			}
		}
	} else if engineState == "error" {
		modelsState = "error"
		modelsDetail = "Engine error"
	}
	out.Models = healthSummaryPart{State: modelsState, Detail: modelsDetail}

	if strings.TrimSpace(s.OpenAIAPIKey) != "" || strings.TrimSpace(s.OpenRouterAPIKey) != "" {
		detail := "Key on file"
		if strings.TrimSpace(s.OpenAIAPIKey) != "" && strings.TrimSpace(s.OpenRouterAPIKey) != "" {
			detail = "OpenAI + OpenRouter keys on file"
		} else if strings.TrimSpace(s.OpenRouterAPIKey) != "" {
			detail = "OpenRouter key on file"
		}
		out.OpenAI = healthSummaryPart{State: "ok", Detail: detail}
	} else {
		out.OpenAI = healthSummaryPart{State: "pending", Detail: "No API key (Settings → OpenAI)"}
	}

	_ = json.NewEncoder(w).Encode(out)
}

func (h *Host) handleEngineLoad(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s := h.readSettingsFromDisk()
	need := needsLocalModels(s)
	if !need.Any() {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":        "ready",
			"device":        "cloud",
			"device_detail": "Cloud pipeline — local Whisper/NLLB not required",
		})
		return
	}
	if err := engine.LoadModelsWithOptions(r.Context(), engine.LoadOptions{
		Whisper: need.Whisper,
		NLLB:    need.NLLB,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	engine.TryFetchLoadSnapshot(r.Context())
	snap := engine.LastLoadSnapshot()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snap)
}
