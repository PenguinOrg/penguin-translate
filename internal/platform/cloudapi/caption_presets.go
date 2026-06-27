package cloudapi

func CaptionPresets() map[string]any {
	return map[string]any{
		"openai": map[string]any{
			"split_transcribe": []string{"gpt-4o-mini-transcribe", "gpt-4o-transcribe"},
			"split_translate":  []string{"gpt-4o-mini", "gpt-4o"},
			"split_diarize":    []string{"gpt-4o-transcribe-diarize"},
			"multimodal":       []string{"gpt-4o-mini"},
		},
		"openrouter": map[string]any{
			"split_transcribe": []string{"qwen/qwen3-asr-flash-2026-02-10", "mistralai/voxtral-mini-transcribe", "openai/whisper-large-v3", "openai/whisper-1"},
			"split_translate":  []string{"openai/gpt-4o-mini", "google/gemini-2.0-flash-lite-001", "xiaomi/mimo-v2.5"},
			"split_diarize":    []string{},
			"multimodal":       []string{"xiaomi/mimo-v2.5", "google/gemini-2.5-flash-lite", "google/gemini-2.5-flash", "mistralai/voxtral-small-24b-2507", "openai/gpt-audio-mini"},
		},
		"dashscope": map[string]any{
			"split_transcribe": []string{"qwen3-asr-flash", "qwen3-asr-flash-2026-02-10"},
			"split_translate":  []string{"qwen-flash", "qwen-plus", "qwen-turbo"},
			"split_diarize":    []string{},
			"multimodal":       []string{"qwen3-omni-flash", "qwen-omni-turbo"},
		},
	}
}
