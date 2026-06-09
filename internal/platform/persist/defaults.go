package persist

import (
	"encoding/json"
	"strings"

	"translation-overlay/internal/platform/domain"
)

const defaultEngineURL = "http://127.0.0.1:8745"

func Default() domain.Settings {
	st := domain.DefaultSettings(defaultEngineURL)
	normalize(&st)
	return st
}

func normalize(st *domain.Settings) {
	if strings.TrimSpace(st.Window.NLLBBaseURL) == "" {
		st.Window.NLLBBaseURL = defaultEngineURL
	}
	if st.MicTranslate.Plugins == nil {
		st.MicTranslate.Plugins = map[string]json.RawMessage{}
	}
	if strings.TrimSpace(st.OpenRouterBaseURL) == "" {
		st.OpenRouterBaseURL = "https://openrouter.ai/api/v1"
	}
	sanitizeModelStrings(st)
}

func sanitizeModelStrings(st *domain.Settings) {
	for _, p := range []*string{
		&st.MicTranslate.OpenAIForwardModel,
		&st.MicTranslate.OpenAIBacktransModel,
		&st.MicTranslate.OpenAITranscribeModel,
		&st.MicTranslate.OpenAIWhisperModel,
		&st.MicTranslate.OpenRouterTranscribeModel,
		&st.MicTranslate.TranscribeModel,
		&st.MicTranslate.TranslateModel,
		&st.MicTranslate.MultimodalModel,
		&st.Audio.TranscribeModel,
		&st.Audio.DiarizeModel,
		&st.Audio.TranslateModel,
		&st.Audio.MultimodalModel,
	} {
		if !isCleanText(*p) {
			*p = ""
		}
	}
}

func isCleanText(s string) bool {
	for _, r := range s {
		if r == '�' || (r < 0x20 && r != '\t') {
			return false
		}
	}
	return true
}
