package host

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	captionpkg "translation-overlay/internal/feature/audio/infra/caption"
	audiosys "translation-overlay/internal/platform/audio"
	"translation-overlay/internal/platform/cloudapi"
)

func formTruthy(values map[string][]string, key string) bool {
	for _, v := range values[key] {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "1" || v == "true" || v == "yes" || v == "on" {
			return true
		}
	}
	return false
}

func firstFormValue(values map[string][]string, key string) string {
	for _, v := range values[key] {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func (h *Host) handleNativeTranscribeSegment(w http.ResponseWriter, r *http.Request) {
	t0 := time.Now()
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field", http.StatusBadRequest)
		return
	}
	wav, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	receiveUS := usSince(t0)

	s := h.readSettingsFromDisk()
	s = normalizeSettings(s)

	provider := strings.ToLower(strings.TrimSpace(s.APIProvider))
	if provider == "" {
		provider = "openrouter"
	}
	lang := strings.ToLower(strings.TrimSpace(s.PrimaryLanguage))
	if v := strings.ToLower(firstFormValue(r.MultipartForm.Value, "language")); v == "auto" {
		lang = ""
	} else if v != "" {
		lang = v
	}
	if len(lang) >= 2 {
		lang = lang[:2]
	}
	pipe := strings.ToLower(strings.TrimSpace(s.PipelineMode))
	if pipe != "multimodal" {
		pipe = "split"
	}
	sec := s.SegmentTimeoutSec
	if sec <= 0 {
		sec = 5
	}
	timeout := time.Duration(sec) * time.Second

	tm := strings.TrimSpace(s.TranscribeModel)
	dm := strings.TrimSpace(s.DiarizeModel)
	trm := strings.TrimSpace(s.TranslateModel)
	mm := strings.TrimSpace(s.MultimodalModel)
	if tm == "" {
		switch provider {
		case "openrouter":
			tm = "qwen/qwen3-asr-flash-2026-02-10"
		case "dashscope":
			tm = "qwen3-asr-flash"
		default:
			tm = "gpt-4o-mini-transcribe"
		}
	}
	if dm == "" {
		dm = "gpt-4o-transcribe-diarize"
	}
	if trm == "" {
		switch provider {
		case "openrouter":
			trm = "google/gemini-2.0-flash-lite-001"
		case "dashscope":
			trm = "qwen-flash"
		default:
			trm = "gpt-4o-mini"
		}
	}
	if mm == "" {
		mm = "xiaomi/mimo-v2-flash"
	}

	var hintLangs []string
	if lang != "" {
		hintLangs = []string{lang}
	} else if full, err := h.repo.Load(); err == nil && len(full.MicTranslate.OtherLanguages) > 0 {
		hintLangs = full.MicTranslate.OtherLanguages
	} else if s.PrimaryLanguage != "" {
		hintLangs = []string{s.PrimaryLanguage}
	}
	captionContext := captionpkg.BuildCaptionContext(s.ContextEnabled, hintLangs, s.ContextHint)
	translateContext := captionpkg.BuildTranslateContext(s.ContextEnabled, s.ContextHint)

	req := captionpkg.SegmentRequest{
		WAV:              wav,
		WantDiarize:      formTruthy(r.MultipartForm.Value, "diarize"),
		WantTranslate:    formTruthy(r.MultipartForm.Value, "translate_to_en"),
		VROverlayOn:      s.OpenVROverlayEnabled,
		Language:         lang,
		Context:          captionContext,
		TranslateContext: translateContext,
		Pipeline:         pipe,
		Provider:         provider,
		TranscribeModel:  tm,
		DiarizeModel:     dm,
		TranslateModel:   trm,
		MultimodalModel:  mm,
		Timeout:          timeout,
		Creds: cloudapi.Credentials{
			OpenAIKey:      s.OpenAIAPIKey,
			OpenAIBase:     s.OpenAIBaseURL,
			OpenRouterKey:  s.OpenRouterAPIKey,
			OpenRouterBase: s.OpenRouterBaseURL,
			DashScopeKey:   s.DashScopeAPIKey,
			DashScopeBase:  s.DashScopeBaseURL,
			APIProvider:    provider,
		},
	}
	tTranscribe := time.Now()
	resp, err := captionpkg.TranscribeSegment(req)
	transcribeTotalUS := usSince(tTranscribe)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	wantTranslate := req.WantTranslate

	transcribeUS := resp.TimingsUS["transcribe"]
	translateUS := resp.TimingsUS["translate"]
	if transcribeUS == 0 && translateUS == 0 {
		transcribeUS = transcribeTotalUS
	}
	goSpans := stageSpans{
		"receive":    receiveUS,
		"transcribe": transcribeUS,
		"translate":  translateUS,
	}

	body, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
	if len(body) > 0 {
		payload := append([]byte(nil), body...)
		go pushOverlayCaptionFromTranscribeJSON(payload, wantTranslate, goSpans)
	}
}

func handleNativeLoopbackHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, audiosys.NativeLoopbackBaseURL()+"/health", nil)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		if r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		}
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		if r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		w.WriteHeader(http.StatusServiceUnavailable)
		if r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "sidecar unhealthy"})
		}
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, resp.Body)
}

func handleNativeLoopbackDevices(w http.ResponseWriter, _ *http.Request) {
	devs, err := listNativeLoopbackDevices()
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"devices": []any{},
			"error":   err.Error(),
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"devices": devs})
}

func listNativeLoopbackDevices() ([]map[string]any, error) {
	devs, err := nativeLoopbackDevices()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(devs))
	for i, d := range devs {
		out[i] = map[string]any{
			"id":          d.ID,
			"name":        d.Name,
			"is_default":  d.IsDefault,
			"loopback_ok": d.LoopbackOK,
		}
	}
	return out, nil
}

func loopbackStartAckJSON() ([]byte, error) {
	return json.Marshal(map[string]string{
		"status": "capturing",
		"device": nativeLoopbackLabel(),
	})
}

func loopbackErrorJSON(msg string) []byte {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return b
}

func loopbackCaptureLostJSON(msg string) []byte {
	b, _ := json.Marshal(map[string]string{"event": "capture_lost", "error": msg})
	return b
}
