package domain

import "strings"

func NormalizeWindowSettings(w *WindowSettings) {
	if w.PollIntervalMS <= 0 {
		w.PollIntervalMS = 1500
	}
	if strings.TrimSpace(w.OpenAIModel) == "" {
		w.OpenAIModel = "gpt-4o-mini"
	}
	if strings.TrimSpace(w.OpenRouterModel) == "" {
		w.OpenRouterModel = "openai/gpt-4o-mini"
	}
	switch strings.ToLower(strings.TrimSpace(w.TranslateBackend)) {
	case "nllb", "local":
		w.TranslateBackend = "nllb"
	case "openrouter", "or":
		w.TranslateBackend = "openrouter"
	default:
		w.TranslateBackend = "openai"
	}
	if strings.TrimSpace(w.Hotkey) == "" {
		w.Hotkey = "F9"
	}
	if w.VRHUDDistanceM <= 0 {
		w.VRHUDDistanceM = 1.6
	}
	if w.VRHUDWidthM <= 0 {
		w.VRHUDWidthM = 1.2
	}
	if w.MaxBatchLines <= 0 {
		w.MaxBatchLines = 8
	}
	if w.MaxBatchLines > 50 {
		w.MaxBatchLines = 50
	}
	if w.MaxBatchChars <= 0 {
		w.MaxBatchChars = 3500
	}
	if w.MaxParallelRequests <= 0 {
		w.MaxParallelRequests = 4
	}
	if w.MaxParallelRequests > 8 {
		w.MaxParallelRequests = 8
	}
}
