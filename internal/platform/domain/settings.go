package domain

import "encoding/json"

type Settings struct {
	OpenAIAPIKey      string               `json:"openai_api_key"`
	OpenAIBaseURL     string               `json:"openai_base_url"`
	OpenRouterAPIKey  string               `json:"openrouter_api_key"`
	OpenRouterBaseURL string               `json:"openrouter_base_url"`
	DashScopeAPIKey   string               `json:"dashscope_api_key"`
	DashScopeBaseURL  string               `json:"dashscope_base_url"`
	MicTranslate      MicTranslateSettings `json:"practice"`
	Window            WindowSettings       `json:"window"`
	Audio             AudioSettings        `json:"audio"`
}

type MicTranslateSettings struct {
	ForwardTranslator         string                     `json:"forward_translator"`
	OpenAIForwardModel        string                     `json:"openai_forward_model"`
	OpenAIBacktransModel      string                     `json:"openai_backtrans_model"`
	EnglishASREngine          string                     `json:"english_asr_engine"`
	OpenAITranscribeModel     string                     `json:"openai_transcribe_model"`
	OpenAIWhisperModel        string                     `json:"openai_whisper_model"`
	OpenRouterTranscribeModel string                     `json:"openrouter_transcribe_model"`
	TranscribeModel           string                     `json:"transcribe_model"`
	TranslateModel            string                     `json:"translate_model"`
	JaRepeatASREngine         string                     `json:"ja_repeat_asr_engine"`
	Backtranslate             string                     `json:"backtranslate"`
	ScoreThreshold            int                        `json:"score_threshold"`
	OutputDeviceName          string                     `json:"output_device_name"`
	TTSEngine                 string                     `json:"tts_engine"`
	OpenAITTSModel            string                     `json:"openai_tts_model"`
	TTSVoiceName              string                     `json:"tts_voice_name"`
	OpenAITTSInstructions     string                     `json:"openai_tts_instructions"`
	TargetLanguage            string                     `json:"target_language"`
	MyLanguage                string                     `json:"my_language"`
	OtherLanguages            []string                   `json:"other_languages"`
	Plugins                   map[string]json.RawMessage `json:"plugins"`
	ContinuousTranslate       bool                       `json:"continuous_translate"`
	MicSensitivity            int                        `json:"mic_sensitivity"`
	VadSilenceMs              int                        `json:"vad_silence_ms"`
	VadSilenceContinuousMs    int                        `json:"vad_silence_continuous_ms"`
	ContinuousTTSRepeat       bool                       `json:"continuous_tts_repeat"`
	TTSRepeatDebounceMs       int                        `json:"tts_repeat_debounce_ms"`
	WhisperGPU                string                     `json:"whisper_gpu"`
	NLLBGPU                   string                     `json:"nllb_gpu"`
	PipelineMode              string                     `json:"pipeline_mode"`
	APIProvider               string                     `json:"api_provider"`
	MultimodalModel           string                     `json:"multimodal_model"`
	PracticeEnabled           bool                       `json:"practice_enabled"`
	SessionActive             bool                       `json:"session_active"`
}

type WindowSettings struct {
	OpenAIModel         string   `json:"openai_model"`
	OpenRouterModel     string   `json:"openrouter_model"`
	TranslateBackend    string   `json:"translate_backend"`
	NLLBBaseURL         string   `json:"nllb_base_url"`
	PollIntervalMS      int      `json:"poll_interval_ms"`
	WindowHWND          uint64   `json:"window_hwnd"`
	WindowTitle         string   `json:"window_title"`
	WindowProcessName   string   `json:"window_process_name"`
	OCRDir              string   `json:"ocr_dir"`
	SourceLang          string   `json:"source_lang"`
	OverlayEnabled      bool     `json:"overlay_enabled"`
	VROverlayEnabled    bool     `json:"vr_overlay_enabled"`
	VRHUDDistanceM      float64  `json:"vr_hud_distance_m"`
	VRHUDWidthM         float64  `json:"vr_hud_width_m"`
	VRBillboardFollow   bool     `json:"vr_billboard_follow"`
	Hotkey              string   `json:"hotkey"`
	SkipWords           []string `json:"skip_words"`
	MaxBatchLines       int      `json:"max_batch_lines"`
	MaxBatchChars       int      `json:"max_batch_chars"`
	MaxParallelRequests int      `json:"max_parallel_requests"`
	OverlayShowSource   bool     `json:"overlay_show_source"`
	SessionActive       bool     `json:"session_active"`
}

type AudioSettings struct {
	APIProvider                string  `json:"api_provider"`
	PipelineMode               string  `json:"pipeline_mode"`
	TranscribeModel            string  `json:"transcribe_model"`
	DiarizeModel               string  `json:"diarize_model"`
	TranslateModel             string  `json:"translate_model"`
	MultimodalModel            string  `json:"multimodal_model"`
	DiarizeByDefault           bool    `json:"diarize_by_default"`
	TranslateByDefault         bool    `json:"translate_by_default"`
	PrimaryLanguage            string  `json:"primary_language"`
	ContextEnabled             bool    `json:"context_enabled"`
	ContextHint                string  `json:"context_hint"`
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

func DefaultSettings(engineBaseURL string) Settings {
	return Settings{
		OpenAIBaseURL:     "",
		OpenRouterBaseURL: "https://openrouter.ai/api/v1",
		DashScopeBaseURL:  "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
		MicTranslate: MicTranslateSettings{
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
			ScoreThreshold:            100,
			TargetLanguage:            "jp",
			MyLanguage:                "en",
			OtherLanguages:            []string{"ja"},
			Plugins:                   map[string]json.RawMessage{},
			MicSensitivity:            18,
			VadSilenceMs:              1500,
			VadSilenceContinuousMs:    2700,
			TTSRepeatDebounceMs:       1500,
			TTSEngine:                 "openrouter",
			TTSVoiceName:              "coral",
			PipelineMode:              "split",
			APIProvider:               "openrouter",
			MultimodalModel:           "xiaomi/mimo-v2-flash",
		},
		Window: WindowSettings{
			OpenAIModel:         "gpt-4o-mini",
			OpenRouterModel:     "openai/gpt-4o-mini",
			TranslateBackend:    "openrouter",
			NLLBBaseURL:         engineBaseURL,
			PollIntervalMS:      1500,
			SourceLang:          "auto",
			OverlayEnabled:      true,
			Hotkey:              "F9",
			MaxBatchLines:       8,
			MaxBatchChars:       3500,
			MaxParallelRequests: 4,
			VRHUDDistanceM:      1.6,
			VRHUDWidthM:         1.2,
		},
		Audio: AudioSettings{
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
	}
}
