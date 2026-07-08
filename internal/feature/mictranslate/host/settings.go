package host

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"translation-overlay/internal/feature/mictranslate/infra/plugin"
	"translation-overlay/internal/platform/domain"
	"translation-overlay/internal/platform/lang/languages"
)

const settingsFileName = "settings.json"

type settingsFile struct {
	domain.MicTranslateSettings

	OpenAIAPIKey      string `json:"openai_api_key"`
	OpenAIBaseURL     string `json:"openai_base_url"`
	OpenRouterAPIKey  string `json:"openrouter_api_key"`
	OpenRouterBaseURL string `json:"openrouter_base_url"`
	DeepgramAPIKey    string `json:"deepgram_api_key"`
	DeepgramBaseURL   string `json:"deepgram_base_url"`
	DashScopeAPIKey   string `json:"dashscope_api_key"`
	DashScopeBaseURL  string `json:"dashscope_base_url"`
	AzureSpeechKey    string `json:"azure_speech_key"`
}

func (h *Host) settingsFilePath() (string, error) {
	d, err := h.appDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, settingsFileName), nil
}

func defaultSettingsFile() settingsFile {
	return settingsFile{
		MicTranslateSettings: domain.MicTranslateSettings{
			ForwardTranslator:         "openai",
			OpenAIForwardModel:        "gpt-4o-mini",
			OpenAIBacktransModel:      "gpt-4o-mini",
			EnglishASREngine:          "openrouter",
			OpenAITranscribeModel:     "gpt-4o-mini-transcribe",
			OpenAIWhisperModel:        "whisper-1",
			OpenRouterTranscribeModel: "qwen/qwen3-asr-flash-2026-02-10",
			TranscribeModel:           "qwen/qwen3-asr-flash-2026-02-10",
			TranslateModel:            "openai/gpt-4o-mini",
			JaRepeatASREngine:         "openrouter",
			Backtranslate:             "local",
			ScoreThreshold:            70,
			AssessmentMode:            "basic",
			TargetLanguage:            "jp",
			MyLanguage:                "en",
			OtherLanguages:            []string{"ja"},
			Plugins:                   map[string]json.RawMessage{},
			MicSensitivity:            18,
			VadSilenceMs:              1500,
			VadSilenceContinuousMs:    2700,
			ContinuousTTSRepeat:       true,
			TTSRepeatDebounceMs:       1500,
			TTSEngine:                 "openrouter",
			OpenAITTSModel:            "google/gemini-3.1-flash-tts-preview",
			TTSVoiceName:              "Kore",
			OutputDeviceName:          "CABLE Input",
			PipelineMode:              "split",
			APIProvider:               "openrouter",
			MultimodalModel:           "xiaomi/mimo-v2-flash",
		},
		OpenRouterBaseURL: "https://openrouter.ai/api/v1",
	}
}

func clampVadSilenceMs(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampThreshold(v int) int {
	if v < 50 {
		return 50
	}
	if v > 100 {
		return 100
	}
	return v
}

func normalizeTTSEngine(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "openai":
		return "openai"
	case "deepgram":
		return "deepgram"
	case "dashscope":
		return "dashscope"
	default:
		return "openrouter"
	}
}

// Retired/unsupported TTS model ids that providers now 400 on (a dead OpenRouter
// snapshot, or models we removed because their voices don't fit). Heal them to the
// verified OpenRouter-Gemini default so a stale settings.json doesn't break "Hear it".
func healTTSModel(s settingsFile) settingsFile {
	switch strings.TrimSpace(s.OpenAITTSModel) {
	case "openai/gpt-4o-mini-tts-2025-12-15", "gpt-4o-mini-tts-2025-12-15",
		"hexgrad/kokoro-82m", "mistralai/voxtral-mini-tts-2603":
		s.TTSEngine = "openrouter"
		s.OpenAITTSModel = "google/gemini-3.1-flash-tts-preview"
		s.TTSVoiceName = "Kore"
	}
	return s
}

func normalizeAssessmentMode(v string) string {
	if strings.ToLower(strings.TrimSpace(v)) == "azure" {
		return "azure"
	}
	return "basic"
}

func normalizeAPIProvider(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "openai":
		return "openai"
	default:
		return "openrouter"
	}
}

func normalizePipelineMode(v string) string {
	if strings.ToLower(strings.TrimSpace(v)) == "multimodal" {
		return "multimodal"
	}
	return "split"
}

func normalizeASREngine(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "openai", "openai_gpt", "gpt":
		return "openai"
	case "openai_whisper", "openai-whisper", "whisper-api", "whisper_api":
		return "openai_whisper"
	case "openrouter", "or":
		return "openrouter"
	case "deepgram", "dg":
		return "deepgram"
	case "dashscope", "ds":
		return "dashscope"
	default:
		return "whisper"
	}
}

func normalizeConversationLanguages(m *domain.MicTranslateSettings) {
	my := languages.CanonicalID(m.MyLanguage)
	if my == "" {
		my = "en"
	}
	m.MyLanguage = my

	seen := map[string]bool{}
	others := make([]string, 0, len(m.OtherLanguages))
	for _, id := range m.OtherLanguages {
		c := languages.CanonicalID(id)
		if c == "" || c == my || seen[c] {
			continue
		}
		seen[c] = true
		others = append(others, c)
	}
	if len(others) == 0 {
		if mig := languages.CanonicalID(m.TargetLanguage); mig != "" && mig != my {
			others = append(others, mig)
		} else if my != "ja" {
			others = append(others, "ja")
		} else {
			others = append(others, "en")
		}
	}
	m.OtherLanguages = others
}

func seedConversationLanguagesFromLegacy(st *domain.Settings) {
	if len(st.MicTranslate.OtherLanguages) > 0 {
		return
	}
	my := languages.CanonicalID(st.MicTranslate.MyLanguage)
	if my == "" {
		my = "en"
	}
	for _, cand := range []string{st.Audio.PrimaryLanguage, st.MicTranslate.TargetLanguage} {
		c := languages.CanonicalID(cand)
		if c == my {
			continue
		}
		if _, ok := languages.Lang(c); ok {
			st.MicTranslate.OtherLanguages = []string{c}
			return
		}
	}
}

func normalizeSettings(s settingsFile) settingsFile {
	ft := strings.ToLower(strings.TrimSpace(s.ForwardTranslator))
	if ft != "openai" {
		ft = "nllb"
	}
	s.ForwardTranslator = ft

	if strings.TrimSpace(s.OpenAIForwardModel) == "" {
		s.OpenAIForwardModel = "gpt-4o-mini"
	}
	if strings.TrimSpace(s.OpenAIBacktransModel) == "" {
		s.OpenAIBacktransModel = "gpt-4o-mini"
	}
	s.OpenAIBaseURL = strings.TrimSpace(s.OpenAIBaseURL)

	s.EnglishASREngine = normalizeASREngine(s.EnglishASREngine)
	if strings.TrimSpace(s.OpenAITranscribeModel) == "" {
		s.OpenAITranscribeModel = "gpt-4o-mini-transcribe"
	}
	if strings.TrimSpace(s.OpenAIWhisperModel) == "" {
		s.OpenAIWhisperModel = "whisper-1"
	}
	s.OpenRouterBaseURL = strings.TrimSpace(s.OpenRouterBaseURL)
	if s.OpenRouterBaseURL == "" {
		s.OpenRouterBaseURL = "https://openrouter.ai/api/v1"
	}
	if strings.TrimSpace(s.OpenRouterTranscribeModel) == "" {
		s.OpenRouterTranscribeModel = "qwen/qwen3-asr-flash-2026-02-10"
	}
	if strings.TrimSpace(s.DeepgramBaseURL) == "" {
		s.DeepgramBaseURL = "https://api.deepgram.com"
	}
	if strings.TrimSpace(s.TranscribeModel) == "" {
		if s.EnglishASREngine == "openrouter" {
			s.TranscribeModel = s.OpenRouterTranscribeModel
		} else if s.EnglishASREngine == "deepgram" {
			s.TranscribeModel = "nova-2"
		} else if s.EnglishASREngine == "dashscope" {
			s.TranscribeModel = "qwen3-asr-flash"
		} else if s.EnglishASREngine == "openai" || s.EnglishASREngine == "openai_whisper" {
			if s.EnglishASREngine == "openai_whisper" {
				s.TranscribeModel = s.OpenAIWhisperModel
			} else {
				s.TranscribeModel = s.OpenAITranscribeModel
			}
		} else {
			s.TranscribeModel = s.OpenRouterTranscribeModel
		}
	}
	if strings.TrimSpace(s.TranslateModel) == "" {
		if s.ForwardTranslator == "openai" && s.APIProvider == "openrouter" {
			s.TranslateModel = "openai/gpt-4o-mini"
		} else {
			s.TranslateModel = s.OpenAIForwardModel
		}
	}

	if s.EnglishASREngine == "" {
		s.EnglishASREngine = "whisper"
	}
	s.JaRepeatASREngine = s.EnglishASREngine

	bt := strings.ToLower(strings.TrimSpace(s.Backtranslate))
	if bt != "none" && bt != "local" && bt != "openai" {
		bt = "local"
	}
	s.Backtranslate = bt

	if s.ScoreThreshold == 0 {
		s.ScoreThreshold = 70
	}
	s.ScoreThreshold = clampThreshold(s.ScoreThreshold)
	s.AssessmentMode = normalizeAssessmentMode(s.AssessmentMode)
	s.AzureSpeechRegion = strings.TrimSpace(s.AzureSpeechRegion)

	s.OutputDeviceName = strings.TrimSpace(s.OutputDeviceName)
	if s.OutputDeviceName == "" {
		s.OutputDeviceName = "CABLE Input"
	}
	s.TTSEngine = normalizeTTSEngine(s.TTSEngine)
	s = healTTSModel(s)
	if strings.TrimSpace(s.OpenAITTSModel) == "" {
		switch s.TTSEngine {
		case "openai":
			s.OpenAITTSModel = "gpt-4o-mini-tts"
		case "deepgram":
			s.OpenAITTSModel = "aura-2-thalia-en"
		case "dashscope":
			s.OpenAITTSModel = "qwen3-tts-flash"
		default:
			s.OpenAITTSModel = "google/gemini-3.1-flash-tts-preview"
		}
	}
	s.TTSVoiceName = strings.TrimSpace(s.TTSVoiceName)
	if s.TTSVoiceName == "" {
		s.TTSVoiceName = "coral"
	}
	s.OpenAITTSInstructions = strings.TrimSpace(s.OpenAITTSInstructions)
	s.TargetLanguage = languages.NormalizeID(s.TargetLanguage)
	normalizeConversationLanguages(&s.MicTranslateSettings)
	if s.MicSensitivity == 0 {
		s.MicSensitivity = 18
	}
	s.MicSensitivity = clampVadSilenceMs(s.MicSensitivity, 5, 50)
	if s.VadSilenceMs == 0 {
		s.VadSilenceMs = 1500
	}
	s.VadSilenceMs = clampVadSilenceMs(s.VadSilenceMs, 300, 5000)
	if s.VadSilenceContinuousMs == 0 {
		s.VadSilenceContinuousMs = 2700
	}
	s.VadSilenceContinuousMs = clampVadSilenceMs(s.VadSilenceContinuousMs, 300, 8000)
	if s.TTSRepeatDebounceMs == 0 {
		s.TTSRepeatDebounceMs = 1500
		s.ContinuousTTSRepeat = true
	}
	s.TTSRepeatDebounceMs = clampVadSilenceMs(s.TTSRepeatDebounceMs, 500, 8000)
	s.WhisperGPU = strings.TrimSpace(s.WhisperGPU)
	s.NLLBGPU = strings.TrimSpace(s.NLLBGPU)
	s.PipelineMode = normalizePipelineMode(s.PipelineMode)
	s.APIProvider = normalizeAPIProvider(s.APIProvider)
	if strings.TrimSpace(s.MultimodalModel) == "" {
		s.MultimodalModel = "xiaomi/mimo-v2-flash"
	}
	if s.Plugins == nil {
		s.Plugins = map[string]json.RawMessage{}
	}
	plugin.Default.ApplyAllConfigs(s.Plugins)
	return s
}

type settingsPublicJSON struct {
	ForwardTranslator         string                    `json:"forward_translator"`
	OpenAIKeyConfigured       bool                      `json:"openai_key_configured"`
	OpenAIBaseURL             string                    `json:"openai_base_url"`
	OpenAIForwardModel        string                    `json:"openai_forward_model"`
	OpenAIBacktransModel      string                    `json:"openai_backtrans_model"`
	EnglishASREngine          string                    `json:"english_asr_engine"`
	OpenAITranscribeModel     string                    `json:"openai_transcribe_model"`
	OpenAIWhisperModel        string                    `json:"openai_whisper_model"`
	OpenRouterKeyConfigured   bool                      `json:"openrouter_key_configured"`
	OpenRouterBaseURL         string                    `json:"openrouter_base_url"`
	OpenRouterTranscribeModel string                    `json:"openrouter_transcribe_model"`
	TranscribeModel           string                    `json:"transcribe_model"`
	TranslateModel            string                    `json:"translate_model"`
	JaRepeatASREngine         string                    `json:"ja_repeat_asr_engine"`
	Backtranslate             string                    `json:"backtranslate"`
	ScoreThreshold            int                       `json:"score_threshold"`
	AssessmentMode            string                    `json:"assessment_mode"`
	AzureSpeechRegion         string                    `json:"azure_speech_region"`
	OutputDeviceName          string                    `json:"output_device_name"`
	TTSEngine                 string                    `json:"tts_engine"`
	OpenAITTSModel            string                    `json:"openai_tts_model"`
	TTSVoiceName              string                    `json:"tts_voice_name"`
	OpenAITTSInstructions     string                    `json:"openai_tts_instructions"`
	TargetLanguage            string                    `json:"target_language"`
	MyLanguage                string                    `json:"my_language"`
	OtherLanguages            []string                  `json:"other_languages"`
	Plugins                   map[string]map[string]any `json:"plugins"`
	ContinuousTranslate       bool                      `json:"continuous_translate"`
	MicSensitivity            int                       `json:"mic_sensitivity"`
	VadSilenceMs              int                       `json:"vad_silence_ms"`
	VadSilenceContinuousMs    int                       `json:"vad_silence_continuous_ms"`
	ContinuousTTSRepeat       bool                      `json:"continuous_tts_repeat"`
	TTSRepeatDebounceMs       int                       `json:"tts_repeat_debounce_ms"`
	SettingsPath              string                    `json:"settings_path"`
	WhisperGPU                string                    `json:"whisper_gpu"`
	NLLBGPU                   string                    `json:"nllb_gpu"`
	PipelineMode              string                    `json:"pipeline_mode"`
	APIProvider               string                    `json:"api_provider"`
	MultimodalModel           string                    `json:"multimodal_model"`
	PracticeEnabled           bool                      `json:"practice_enabled"`
	SessionActive             bool                      `json:"session_active"`
}

type settingsPostJSON struct {
	ForwardTranslator         string          `json:"forward_translator"`
	OpenAIForwardModel        string          `json:"openai_forward_model"`
	OpenAIBacktransModel      string          `json:"openai_backtrans_model"`
	EnglishASREngine          string          `json:"english_asr_engine"`
	OpenAITranscribeModel     string          `json:"openai_transcribe_model"`
	OpenAIWhisperModel        string          `json:"openai_whisper_model"`
	OpenRouterTranscribeModel string          `json:"openrouter_transcribe_model"`
	TranscribeModel           string          `json:"transcribe_model"`
	TranslateModel            string          `json:"translate_model"`
	JaRepeatASREngine         string          `json:"ja_repeat_asr_engine"`
	Backtranslate             string          `json:"backtranslate"`
	ScoreThreshold            int             `json:"score_threshold"`
	AssessmentMode            string          `json:"assessment_mode"`
	AzureSpeechRegion         string          `json:"azure_speech_region"`
	OutputDeviceName          string          `json:"output_device_name"`
	TTSEngine                 string          `json:"tts_engine"`
	OpenAITTSModel            string          `json:"openai_tts_model"`
	TTSVoiceName              string          `json:"tts_voice_name"`
	OpenAITTSInstructions     string          `json:"openai_tts_instructions"`
	TargetLanguage            string          `json:"target_language"`
	MyLanguage                string          `json:"my_language"`
	OtherLanguages            []string        `json:"other_languages"`
	Plugins                   json.RawMessage `json:"plugins"`
	ContinuousTranslate       *bool           `json:"continuous_translate"`
	MicSensitivity            int             `json:"mic_sensitivity"`
	VadSilenceMs              int             `json:"vad_silence_ms"`
	VadSilenceContinuousMs    int             `json:"vad_silence_continuous_ms"`
	ContinuousTTSRepeat       *bool           `json:"continuous_tts_repeat"`
	TTSRepeatDebounceMs       int             `json:"tts_repeat_debounce_ms"`
	WhisperGPU                string          `json:"whisper_gpu"`
	NLLBGPU                   string          `json:"nllb_gpu"`
	PipelineMode              string          `json:"pipeline_mode"`
	APIProvider               string          `json:"api_provider"`
	MultimodalModel           string          `json:"multimodal_model"`
	PracticeEnabled           *bool           `json:"practice_enabled"`
	ASREngine                 string          `json:"asr_engine"`
	SessionActive             *bool           `json:"session_active"`
}

func (h *Host) toPublicJSON(s settingsFile) settingsPublicJSON {
	path, _ := h.settingsFilePath()
	return settingsPublicJSON{
		ForwardTranslator:         s.ForwardTranslator,
		OpenAIKeyConfigured:       strings.TrimSpace(s.OpenAIAPIKey) != "",
		OpenAIBaseURL:             s.OpenAIBaseURL,
		OpenAIForwardModel:        s.OpenAIForwardModel,
		OpenAIBacktransModel:      s.OpenAIBacktransModel,
		EnglishASREngine:          s.EnglishASREngine,
		OpenAITranscribeModel:     s.OpenAITranscribeModel,
		OpenAIWhisperModel:        s.OpenAIWhisperModel,
		OpenRouterKeyConfigured:   strings.TrimSpace(s.OpenRouterAPIKey) != "",
		OpenRouterBaseURL:         s.OpenRouterBaseURL,
		OpenRouterTranscribeModel: s.OpenRouterTranscribeModel,
		TranscribeModel:           s.TranscribeModel,
		TranslateModel:            s.TranslateModel,
		JaRepeatASREngine:         s.JaRepeatASREngine,
		Backtranslate:             s.Backtranslate,
		ScoreThreshold:            s.ScoreThreshold,
		AssessmentMode:            s.AssessmentMode,
		AzureSpeechRegion:         s.AzureSpeechRegion,
		OutputDeviceName:          s.OutputDeviceName,
		TTSEngine:                 s.TTSEngine,
		OpenAITTSModel:            s.OpenAITTSModel,
		TTSVoiceName:              s.TTSVoiceName,
		OpenAITTSInstructions:     s.OpenAITTSInstructions,
		TargetLanguage:            s.TargetLanguage,
		MyLanguage:                s.MyLanguage,
		OtherLanguages:            s.OtherLanguages,
		Plugins:                   plugin.Default.PublicConfigs(),
		ContinuousTranslate:       s.ContinuousTranslate,
		MicSensitivity:            s.MicSensitivity,
		VadSilenceMs:              s.VadSilenceMs,
		VadSilenceContinuousMs:    s.VadSilenceContinuousMs,
		ContinuousTTSRepeat:       s.ContinuousTTSRepeat,
		TTSRepeatDebounceMs:       s.TTSRepeatDebounceMs,
		SettingsPath:              path,
		WhisperGPU:                s.WhisperGPU,
		NLLBGPU:                   s.NLLBGPU,
		PipelineMode:              s.PipelineMode,
		APIProvider:               s.APIProvider,
		MultimodalModel:           s.MultimodalModel,
		PracticeEnabled:           s.PracticeEnabled,
		SessionActive:             s.SessionActive,
	}
}

func effectiveBacktranslate(s settingsFile) string {
	if !s.PracticeEnabled {
		return "none"
	}
	bt := strings.ToLower(strings.TrimSpace(s.Backtranslate))
	if bt != "none" && bt != "local" && bt != "openai" {
		return "local"
	}
	return bt
}

type localModelNeeds struct {
	Whisper bool `json:"whisper"`
	NLLB    bool `json:"nllb"`
}

func (n localModelNeeds) Any() bool { return n.Whisper || n.NLLB }

func needsLocalModels(s settingsFile) localModelNeeds {
	need := domain.MicTranslateLocalModelNeeds(micTranslateSettingsFromFile(s))
	return localModelNeeds{Whisper: need.Whisper, NLLB: need.NLLB}
}

func micTranslateSettingsFromFile(s settingsFile) domain.MicTranslateSettings {
	return normalizeSettings(s).MicTranslateSettings
}

func (h *Host) PublicSettings(st domain.Settings) any {
	seedConversationLanguagesFromLegacy(&st)
	return h.toPublicJSON(normalizeSettings(micTranslateFromDomain(st)))
}

func ApplySettingsPatch(st domain.Settings, body []byte) (domain.Settings, error) {
	var in settingsPostJSON
	if err := json.Unmarshal(body, &in); err != nil {
		return st, err
	}
	seedConversationLanguagesFromLegacy(&st)
	cur := normalizeSettings(micTranslateFromDomain(st))
	next := cur
	applyMicTranslatePatch(&next, in)
	applyMicTranslateToDomain(&st, normalizeSettings(next))
	return st, nil
}

func applyMicTranslatePatch(next *settingsFile, in settingsPostJSON) {
	if ft := strings.ToLower(strings.TrimSpace(in.ForwardTranslator)); ft == "openai" || ft == "nllb" {
		next.ForwardTranslator = ft
	}
	if m := strings.TrimSpace(in.OpenAIForwardModel); m != "" {
		next.OpenAIForwardModel = m
	}
	if m := strings.TrimSpace(in.OpenAIBacktransModel); m != "" {
		next.OpenAIBacktransModel = m
	}
	if asr := strings.TrimSpace(in.ASREngine); asr != "" {
		ear := normalizeASREngine(asr)
		next.EnglishASREngine = ear
		next.JaRepeatASREngine = ear
	} else if ear := normalizeASREngine(in.EnglishASREngine); in.EnglishASREngine != "" {
		next.EnglishASREngine = ear
		next.JaRepeatASREngine = ear
	} else if jre := normalizeASREngine(in.JaRepeatASREngine); in.JaRepeatASREngine != "" {
		next.EnglishASREngine = jre
		next.JaRepeatASREngine = jre
	}
	if m := strings.TrimSpace(in.OpenAITranscribeModel); m != "" {
		next.OpenAITranscribeModel = m
	}
	if m := strings.TrimSpace(in.OpenAIWhisperModel); m != "" {
		next.OpenAIWhisperModel = m
	}
	if m := strings.TrimSpace(in.OpenRouterTranscribeModel); m != "" {
		next.OpenRouterTranscribeModel = m
	}
	if m := strings.TrimSpace(in.TranscribeModel); m != "" {
		next.TranscribeModel = m
	}
	if m := strings.TrimSpace(in.TranslateModel); m != "" {
		next.TranslateModel = m
	}
	if in.PracticeEnabled != nil {
		next.PracticeEnabled = *in.PracticeEnabled
	}
	if bt := strings.ToLower(strings.TrimSpace(in.Backtranslate)); bt == "none" || bt == "local" || bt == "openai" {
		next.Backtranslate = bt
	}
	if in.ScoreThreshold != 0 {
		next.ScoreThreshold = clampThreshold(in.ScoreThreshold)
	}
	if in.AssessmentMode != "" {
		next.AssessmentMode = normalizeAssessmentMode(in.AssessmentMode)
	}
	if rg := strings.TrimSpace(in.AzureSpeechRegion); rg != "" {
		next.AzureSpeechRegion = rg
	}
	if name := strings.TrimSpace(in.OutputDeviceName); name != "" {
		next.OutputDeviceName = name
	}
	if in.TTSEngine != "" {
		next.TTSEngine = normalizeTTSEngine(in.TTSEngine)
	}
	if m := strings.TrimSpace(in.OpenAITTSModel); m != "" {
		next.OpenAITTSModel = m
	}
	if v := strings.TrimSpace(in.TTSVoiceName); v != "" {
		next.TTSVoiceName = v
	}
	next.OpenAITTSInstructions = strings.TrimSpace(in.OpenAITTSInstructions)
	if tl := strings.TrimSpace(in.TargetLanguage); tl != "" {
		next.TargetLanguage = languages.NormalizeID(tl)
	}
	if ml := strings.TrimSpace(in.MyLanguage); ml != "" {
		next.MyLanguage = languages.CanonicalID(ml)
	}
	if in.OtherLanguages != nil {
		next.OtherLanguages = in.OtherLanguages
	}
	if in.ContinuousTranslate != nil {
		next.ContinuousTranslate = *in.ContinuousTranslate
	}
	if in.MicSensitivity != 0 {
		next.MicSensitivity = clampVadSilenceMs(in.MicSensitivity, 5, 50)
	}
	if in.VadSilenceMs != 0 {
		next.VadSilenceMs = clampVadSilenceMs(in.VadSilenceMs, 300, 5000)
	}
	if in.VadSilenceContinuousMs != 0 {
		next.VadSilenceContinuousMs = clampVadSilenceMs(in.VadSilenceContinuousMs, 300, 8000)
	}
	if in.ContinuousTTSRepeat != nil {
		next.ContinuousTTSRepeat = *in.ContinuousTTSRepeat
	}
	if in.TTSRepeatDebounceMs != 0 {
		next.TTSRepeatDebounceMs = clampVadSilenceMs(in.TTSRepeatDebounceMs, 500, 8000)
	}
	next.WhisperGPU = strings.TrimSpace(in.WhisperGPU)
	next.NLLBGPU = strings.TrimSpace(in.NLLBGPU)
	if pm := normalizePipelineMode(in.PipelineMode); in.PipelineMode != "" {
		next.PipelineMode = pm
	}
	if ap := normalizeAPIProvider(in.APIProvider); in.APIProvider != "" {
		next.APIProvider = ap
	}
	if m := strings.TrimSpace(in.MultimodalModel); m != "" {
		next.MultimodalModel = m
	}
	if in.SessionActive != nil {
		next.SessionActive = *in.SessionActive
	}
	if len(in.Plugins) > 0 && string(in.Plugins) != "null" {
		var patch map[string]json.RawMessage
		if json.Unmarshal(in.Plugins, &patch) == nil {
			if next.Plugins == nil {
				next.Plugins = map[string]json.RawMessage{}
			}
			for k, v := range patch {
				next.Plugins[k] = v
			}
		}
	}
}
