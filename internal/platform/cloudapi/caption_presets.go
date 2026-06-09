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
			"split_transcribe": []string{"qwen/qwen3-asr-flash-2026-02-10", "openai/whisper-large-v3", "openai/whisper-1"},
			"split_translate":  []string{"openai/gpt-4o-mini", "google/gemini-2.0-flash-lite-001", "xiaomi/mimo-v2-flash"},
			"split_diarize":    []string{},
			"multimodal":       []string{"xiaomi/mimo-v2-flash", "google/gemini-2.5-flash", "google/gemini-2.0-flash-lite-001"},
		},
	}
}
