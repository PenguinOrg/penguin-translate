package host

import (
	"encoding/json"
	"net/http"
	"strings"

	"translation-overlay/internal/platform/domain"
)

type settingsFile struct {
	domain.AudioSettings

	OpenAIAPIKey      string `json:"openai_api_key"`
	OpenAIBaseURL     string `json:"openai_base_url"`
	OpenRouterAPIKey  string `json:"openrouter_api_key"`
	OpenRouterBaseURL string `json:"openrouter_base_url"`
}

func defaultSettingsFile() settingsFile {
	return settingsFile{
		AudioSettings: domain.AudioSettings{
			APIProvider:                "openai",
			PipelineMode:               "split",
			TranscribeModel:            "gpt-4o-mini-transcribe",
			DiarizeModel:               "gpt-4o-transcribe-diarize",
			TranslateModel:             "gpt-4o-mini",
			MultimodalModel:            "xiaomi/mimo-v2-flash",
			PrimaryLanguage:            "ja",
			ChunkProfile:               "sentence",
			DenoiseEnabled:             true,
			DesktopOverlayWidth:        1280,
			DesktopOverlayFontScale:    1.0,
			DesktopOverlayAlign:        "center",
			DesktopOverlayMarginBottom: 72,
			DesktopOverlayMarginX:      24,
			VROverlayWidthM:            1.6,
			VROverlayDistanceM:         1.8,
			VROverlayYOffsetM:          0.0,
			SegmentTimeoutSec:          5,
			VadSensitivity:             15,
			SpeechDetection:            "streaming",
			ClipMinSec:                 0.2,
			ClipMaxSec:                 3.5,
			ClipSilenceMs:              450,
			ClipLongAfterSec:           2.0,
			ClipSilenceLongMs:          180,
		},
		OpenRouterBaseURL: "https://openrouter.ai/api/v1",
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func bodyHasKey(body []byte, key string) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return false
	}
	_, ok := raw[key]
	return ok
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func normalizeOverlayAlign(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "left", "right":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return "center"
	}
}

func normalizeSettings(s settingsFile) settingsFile {
	s.OpenAIBaseURL = strings.TrimSpace(s.OpenAIBaseURL)
	if strings.TrimSpace(s.TranscribeModel) == "" {
		s.TranscribeModel = "gpt-4o-mini-transcribe"
	}
	if strings.TrimSpace(s.DiarizeModel) == "" {
		s.DiarizeModel = "gpt-4o-transcribe-diarize"
	}
	if strings.TrimSpace(s.TranslateModel) == "" {
		s.TranslateModel = "gpt-4o-mini"
	}
	lang := strings.ToLower(strings.TrimSpace(s.PrimaryLanguage))
	if len(lang) < 2 {
		lang = "ja"
	}
	s.PrimaryLanguage = lang[:2]
	cp := strings.ToLower(strings.TrimSpace(s.ChunkProfile))
	if cp == "extended" {
		s.ChunkProfile = "extended"
	} else {
		s.ChunkProfile = "sentence"
	}
	ap := strings.ToLower(strings.TrimSpace(s.APIProvider))
	if ap == "openrouter" {
		s.APIProvider = "openrouter"
	} else {
		s.APIProvider = "openai"
	}
	pm := strings.ToLower(strings.TrimSpace(s.PipelineMode))
	if pm == "multimodal" {
		s.PipelineMode = "multimodal"
	} else {
		s.PipelineMode = "split"
	}
	if strings.TrimSpace(s.MultimodalModel) == "" {
		s.MultimodalModel = "xiaomi/mimo-v2-flash"
	}
	if strings.TrimSpace(s.OpenRouterBaseURL) == "" && s.APIProvider == "openrouter" {
		s.OpenRouterBaseURL = "https://openrouter.ai/api/v1"
	}
	if s.DesktopOverlayWidth <= 0 {
		s.DesktopOverlayWidth = 1280
	}
	s.DesktopOverlayWidth = clampInt(s.DesktopOverlayWidth, 480, 3840)
	if s.DesktopOverlayFontScale <= 0 {
		s.DesktopOverlayFontScale = 1.0
	}
	s.DesktopOverlayFontScale = clampFloat(s.DesktopOverlayFontScale, 0.5, 2.0)
	s.DesktopOverlayAlign = normalizeOverlayAlign(s.DesktopOverlayAlign)
	s.DesktopOverlayMarginBottom = clampInt(s.DesktopOverlayMarginBottom, 0, 800)
	s.DesktopOverlayMarginX = clampInt(s.DesktopOverlayMarginX, 0, 800)
	if s.VROverlayWidthM <= 0 {
		s.VROverlayWidthM = 1.6
	}
	s.VROverlayWidthM = clampFloat(s.VROverlayWidthM, 0.4, 4.0)
	if s.VROverlayDistanceM <= 0 {
		s.VROverlayDistanceM = 1.8
	}
	s.VROverlayDistanceM = clampFloat(s.VROverlayDistanceM, 0.5, 5.0)
	s.VROverlayYOffsetM = clampFloat(s.VROverlayYOffsetM, -1.5, 1.5)
	if s.SegmentTimeoutSec <= 0 {
		s.SegmentTimeoutSec = 5
	}
	s.SegmentTimeoutSec = clampInt(s.SegmentTimeoutSec, 2, 120)
	if s.VadSensitivity <= 0 {
		s.VadSensitivity = 15
	}
	s.VadSensitivity = clampInt(s.VadSensitivity, 1, 100)
	if s.ClipMinSec <= 0 {
		s.ClipMinSec = 0.2
	}
	s.ClipMinSec = clampFloat(s.ClipMinSec, 0.05, 3.0)
	if s.ClipMaxSec <= 0 {
		s.ClipMaxSec = 3.5
	}
	s.ClipMaxSec = clampFloat(s.ClipMaxSec, 0.5, 60.0)
	if s.ClipMaxSec < s.ClipMinSec {
		s.ClipMaxSec = s.ClipMinSec
	}
	if s.ClipSilenceMs <= 0 {
		s.ClipSilenceMs = 450
	}
	s.ClipSilenceMs = clampInt(s.ClipSilenceMs, 80, 4000)
	if s.ClipLongAfterSec <= 0 {
		s.ClipLongAfterSec = 2.0
	}
	s.ClipLongAfterSec = clampFloat(s.ClipLongAfterSec, 0.3, 30.0)
	if s.ClipSilenceLongMs <= 0 {
		s.ClipSilenceLongMs = 180
	}
	s.ClipSilenceLongMs = clampInt(s.ClipSilenceLongMs, 40, 2000)
	return s
}

type settingsPublicJSON struct {
	OpenAIAPIKey               string  `json:"openai_api_key"`
	OpenRouterAPIKey           string  `json:"openrouter_api_key"`
	OpenAIKeyConfigured        bool    `json:"openai_key_configured"`
	OpenRouterKeyConfigured    bool    `json:"openrouter_key_configured"`
	OpenAIBaseURL              string  `json:"openai_base_url"`
	OpenRouterBaseURL          string  `json:"openrouter_base_url"`
	APIProvider                string  `json:"api_provider"`
	PipelineMode               string  `json:"pipeline_mode"`
	TranscribeModel            string  `json:"transcribe_model"`
	DiarizeModel               string  `json:"diarize_model"`
	TranslateModel             string  `json:"translate_model"`
	MultimodalModel            string  `json:"multimodal_model"`
	DiarizeByDefault           bool    `json:"diarize_by_default"`
	TranslateByDefault         bool    `json:"translate_by_default"`
	PrimaryLanguage            string  `json:"primary_language"`
	ChunkProfile               string  `json:"chunk_profile"`
	DenoiseEnabled             bool    `json:"denoise_enabled"`
	DenoiseDebug               bool    `json:"denoise_debug"`
	OpenVROverlayEnabled       bool    `json:"openvr_overlay_enabled"`
	DesktopOverlayEnabled      bool    `json:"desktop_overlay_enabled"`
	DesktopOverlayWidth        int     `json:"desktop_overlay_width"`
	DesktopOverlayFontScale    float64 `json:"desktop_overlay_font_scale"`
	DesktopOverlayAlign        string  `json:"desktop_overlay_align"`
	DesktopOverlayMarginBottom int     `json:"desktop_overlay_margin_bottom"`
	DesktopOverlayMarginX      int     `json:"desktop_overlay_margin_x"`
	VROverlayWidthM            float64 `json:"vr_overlay_width_m"`
	VROverlayDistanceM         float64 `json:"vr_overlay_distance_m"`
	VROverlayYOffsetM          float64 `json:"vr_overlay_y_offset_m"`
	SegmentTimeoutSec          int     `json:"segment_timeout_sec"`
	VadSensitivity             int     `json:"vad_sensitivity"`
	SpeechDetection            string  `json:"speech_detection"`
	ClipMinSec                 float64 `json:"clip_min_sec"`
	ClipMaxSec                 float64 `json:"clip_max_sec"`
	ClipSilenceMs              int     `json:"clip_silence_ms"`
	ClipLongAfterSec           float64 `json:"clip_long_after_sec"`
	ClipSilenceLongMs          int     `json:"clip_silence_long_ms"`
	SessionActive              bool    `json:"session_active"`
}

type settingsPostJSON struct {
	APIProvider                string  `json:"api_provider"`
	PipelineMode               string  `json:"pipeline_mode"`
	TranscribeModel            string  `json:"transcribe_model"`
	DiarizeModel               string  `json:"diarize_model"`
	TranslateModel             string  `json:"translate_model"`
	MultimodalModel            string  `json:"multimodal_model"`
	DiarizeByDefault           bool    `json:"diarize_by_default"`
	TranslateByDefault         bool    `json:"translate_by_default"`
	PrimaryLanguage            string  `json:"primary_language"`
	ChunkProfile               string  `json:"chunk_profile"`
	DenoiseEnabled             bool    `json:"denoise_enabled"`
	DenoiseDebug               bool    `json:"denoise_debug"`
	OpenVROverlayEnabled       bool    `json:"openvr_overlay_enabled"`
	DesktopOverlayEnabled      bool    `json:"desktop_overlay_enabled"`
	DesktopOverlayWidth        int     `json:"desktop_overlay_width"`
	DesktopOverlayFontScale    float64 `json:"desktop_overlay_font_scale"`
	DesktopOverlayAlign        string  `json:"desktop_overlay_align"`
	DesktopOverlayMarginBottom int     `json:"desktop_overlay_margin_bottom"`
	DesktopOverlayMarginX      int     `json:"desktop_overlay_margin_x"`
	VROverlayWidthM            float64 `json:"vr_overlay_width_m"`
	VROverlayDistanceM         float64 `json:"vr_overlay_distance_m"`
	VROverlayYOffsetM          float64 `json:"vr_overlay_y_offset_m"`
	SegmentTimeoutSec          int     `json:"segment_timeout_sec"`
	VadSensitivity             int     `json:"vad_sensitivity"`
	SpeechDetection            string  `json:"speech_detection"`
	ClipMinSec                 float64 `json:"clip_min_sec"`
	ClipMaxSec                 float64 `json:"clip_max_sec"`
	ClipSilenceMs              int     `json:"clip_silence_ms"`
	ClipLongAfterSec           float64 `json:"clip_long_after_sec"`
	ClipSilenceLongMs          int     `json:"clip_silence_long_ms"`
	SessionActive              *bool   `json:"session_active"`
}

func toSettingsPublicJSON(s settingsFile) settingsPublicJSON {
	return settingsPublicJSON{
		OpenAIAPIKey:               s.OpenAIAPIKey,
		OpenRouterAPIKey:           s.OpenRouterAPIKey,
		OpenAIKeyConfigured:        strings.TrimSpace(s.OpenAIAPIKey) != "",
		OpenRouterKeyConfigured:    strings.TrimSpace(s.OpenRouterAPIKey) != "",
		OpenAIBaseURL:              s.OpenAIBaseURL,
		OpenRouterBaseURL:          s.OpenRouterBaseURL,
		APIProvider:                s.APIProvider,
		PipelineMode:               s.PipelineMode,
		TranscribeModel:            s.TranscribeModel,
		DiarizeModel:               s.DiarizeModel,
		TranslateModel:             s.TranslateModel,
		MultimodalModel:            s.MultimodalModel,
		DiarizeByDefault:           s.DiarizeByDefault,
		TranslateByDefault:         s.TranslateByDefault,
		PrimaryLanguage:            s.PrimaryLanguage,
		ChunkProfile:               s.ChunkProfile,
		DenoiseEnabled:             s.DenoiseEnabled,
		DenoiseDebug:               s.DenoiseDebug,
		OpenVROverlayEnabled:       s.OpenVROverlayEnabled,
		DesktopOverlayEnabled:      s.DesktopOverlayEnabled,
		DesktopOverlayWidth:        s.DesktopOverlayWidth,
		DesktopOverlayFontScale:    s.DesktopOverlayFontScale,
		DesktopOverlayAlign:        s.DesktopOverlayAlign,
		DesktopOverlayMarginBottom: s.DesktopOverlayMarginBottom,
		DesktopOverlayMarginX:      s.DesktopOverlayMarginX,
		VROverlayWidthM:            s.VROverlayWidthM,
		VROverlayDistanceM:         s.VROverlayDistanceM,
		VROverlayYOffsetM:          s.VROverlayYOffsetM,
		SegmentTimeoutSec:          s.SegmentTimeoutSec,
		VadSensitivity:             s.VadSensitivity,
		SpeechDetection:            s.SpeechDetection,
		ClipMinSec:                 s.ClipMinSec,
		ClipMaxSec:                 s.ClipMaxSec,
		ClipSilenceMs:              s.ClipSilenceMs,
		ClipLongAfterSec:           s.ClipLongAfterSec,
		ClipSilenceLongMs:          s.ClipSilenceLongMs,
		SessionActive:              s.SessionActive,
	}
}

func applyOverlayLayoutFields(next *settingsFile, in settingsPostJSON, body []byte) {
	if bodyHasKey(body, "desktop_overlay_width") && in.DesktopOverlayWidth > 0 {
		next.DesktopOverlayWidth = in.DesktopOverlayWidth
	}
	if bodyHasKey(body, "desktop_overlay_font_scale") && in.DesktopOverlayFontScale > 0 {
		next.DesktopOverlayFontScale = in.DesktopOverlayFontScale
	}
	if bodyHasKey(body, "desktop_overlay_align") && strings.TrimSpace(in.DesktopOverlayAlign) != "" {
		next.DesktopOverlayAlign = in.DesktopOverlayAlign
	}
	if bodyHasKey(body, "desktop_overlay_margin_bottom") {
		next.DesktopOverlayMarginBottom = in.DesktopOverlayMarginBottom
	}
	if bodyHasKey(body, "desktop_overlay_margin_x") {
		next.DesktopOverlayMarginX = in.DesktopOverlayMarginX
	}
	if bodyHasKey(body, "vr_overlay_width_m") && in.VROverlayWidthM > 0 {
		next.VROverlayWidthM = in.VROverlayWidthM
	}
	if bodyHasKey(body, "vr_overlay_distance_m") && in.VROverlayDistanceM > 0 {
		next.VROverlayDistanceM = in.VROverlayDistanceM
	}
	if bodyHasKey(body, "vr_overlay_y_offset_m") {
		next.VROverlayYOffsetM = in.VROverlayYOffsetM
	}
}

func PublicSettings(st domain.Settings) any {
	return toSettingsPublicJSON(normalizeSettings(audioFromDomain(st)))
}

func ApplySettingsPatch(st domain.Settings, body []byte) (domain.Settings, error) {
	var in settingsPostJSON
	if err := json.Unmarshal(body, &in); err != nil {
		return st, err
	}
	next := normalizeSettings(audioFromDomain(st))
	applyAudioPatch(&next, in, body)
	applyAudioToDomain(&st, normalizeSettings(next))
	return st, nil
}

func SyncOverlayLayout(st domain.Settings) {
	syncOverlayLayoutFromSettings(normalizeSettings(audioFromDomain(st)))
}

func applyAudioPatch(next *settingsFile, in settingsPostJSON, body []byte) {
	ap := strings.ToLower(strings.TrimSpace(in.APIProvider))
	if bodyHasKey(body, "api_provider") && (ap == "openrouter" || ap == "openai") {
		next.APIProvider = ap
	}
	pm := strings.ToLower(strings.TrimSpace(in.PipelineMode))
	if bodyHasKey(body, "pipeline_mode") && (pm == "multimodal" || pm == "split") {
		next.PipelineMode = pm
	}
	if bodyHasKey(body, "transcribe_model") {
		next.TranscribeModel = strings.TrimSpace(in.TranscribeModel)
	}
	if bodyHasKey(body, "diarize_model") {
		next.DiarizeModel = strings.TrimSpace(in.DiarizeModel)
	}
	if bodyHasKey(body, "translate_model") {
		next.TranslateModel = strings.TrimSpace(in.TranslateModel)
	}
	if bodyHasKey(body, "multimodal_model") {
		next.MultimodalModel = strings.TrimSpace(in.MultimodalModel)
	}
	if bodyHasKey(body, "primary_language") {
		next.PrimaryLanguage = strings.TrimSpace(in.PrimaryLanguage)
	}
	cpf := strings.ToLower(strings.TrimSpace(in.ChunkProfile))
	if cpf == "extended" || cpf == "sentence" {
		next.ChunkProfile = cpf
	}
	if in.SegmentTimeoutSec > 0 {
		next.SegmentTimeoutSec = in.SegmentTimeoutSec
	}
	if bodyHasKey(body, "vad_sensitivity") && in.VadSensitivity > 0 {
		next.VadSensitivity = clampInt(in.VadSensitivity, 1, 100)
	}
	if bodyHasKey(body, "speech_detection") {
		switch m := strings.ToLower(strings.TrimSpace(in.SpeechDetection)); m {
		case "rms", "filter", "streaming":
			next.SpeechDetection = m
		}
	}
	if bodyHasKey(body, "clip_min_sec") && in.ClipMinSec > 0 {
		next.ClipMinSec = in.ClipMinSec
	}
	if bodyHasKey(body, "clip_max_sec") && in.ClipMaxSec > 0 {
		next.ClipMaxSec = in.ClipMaxSec
	}
	if bodyHasKey(body, "clip_silence_ms") && in.ClipSilenceMs > 0 {
		next.ClipSilenceMs = in.ClipSilenceMs
	}
	if bodyHasKey(body, "clip_long_after_sec") && in.ClipLongAfterSec > 0 {
		next.ClipLongAfterSec = in.ClipLongAfterSec
	}
	if bodyHasKey(body, "clip_silence_long_ms") && in.ClipSilenceLongMs > 0 {
		next.ClipSilenceLongMs = in.ClipSilenceLongMs
	}
	if in.SessionActive != nil {
		next.SessionActive = *in.SessionActive
	}
	if bodyHasKey(body, "denoise_enabled") {
		next.DenoiseEnabled = in.DenoiseEnabled
	}
	if bodyHasKey(body, "denoise_debug") {
		next.DenoiseDebug = in.DenoiseDebug
	}
	if bodyHasKey(body, "openvr_overlay_enabled") {
		next.OpenVROverlayEnabled = in.OpenVROverlayEnabled
	}
	if bodyHasKey(body, "desktop_overlay_enabled") {
		next.DesktopOverlayEnabled = in.DesktopOverlayEnabled
	}
	if bodyHasKey(body, "diarize_by_default") {
		next.DiarizeByDefault = in.DiarizeByDefault
	}
	if bodyHasKey(body, "translate_by_default") {
		next.TranslateByDefault = in.TranslateByDefault
	}
	applyOverlayLayoutFields(next, in, body)
}

func (h *Host) handleApplyOverlayLayout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	syncOverlayLayoutFromSettings(h.readSettingsFromDisk())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
