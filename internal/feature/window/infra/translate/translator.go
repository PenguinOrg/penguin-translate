package translate

import (
	"strings"

	"translation-overlay/internal/feature/mictranslate/infra/languages"
	"translation-overlay/internal/platform/cloudapi"
	"translation-overlay/internal/platform/domain"
)

type Translator interface {
	ToTargetLine(text, sourceLang string) (LineResult, error)
	ToTargetBatch(lines []string, sourceLang string) ([]LineResult, error)
}

func ocrTarget(st domain.Settings) string {
	t := languages.CanonicalID(st.MicTranslate.MyLanguage)
	if t == "" {
		return "en"
	}
	return t
}

func NewFromSettings(st domain.Settings) Translator {
	w := st.Window
	target := ocrTarget(st)
	switch strings.ToLower(strings.TrimSpace(w.TranslateBackend)) {
	case "nllb", "local":
		c := NewNLLB(w.NLLBBaseURL)
		c.Target = target
		return c
	case "openrouter", "or":
		c := New(cloudapi.Credentials{
			OpenRouterKey:  st.OpenRouterAPIKey,
			OpenRouterBase: st.OpenRouterBaseURL,
			APIProvider:    "openrouter",
		}, w.OpenRouterModel)
		c.Target = target
		return c
	default:
		c := New(cloudapi.Credentials{
			OpenAIKey:   st.OpenAIAPIKey,
			OpenAIBase:  st.OpenAIBaseURL,
			APIProvider: "openai",
		}, w.OpenAIModel)
		c.Target = target
		return c
	}
}

func CacheKey(st domain.Settings) string {
	return withTarget(backendCacheKey(st.Window), ocrTarget(st))
}

func backendCacheKey(w domain.WindowSettings) string {
	switch strings.ToLower(strings.TrimSpace(w.TranslateBackend)) {
	case "nllb", "local":
		return "nllb"
	case "openrouter", "or":
		m := strings.TrimSpace(w.OpenRouterModel)
		if m == "" {
			return "openrouter:openai/gpt-4o-mini"
		}
		return "openrouter:" + m
	default:
		m := strings.TrimSpace(w.OpenAIModel)
		if m == "" {
			return "gpt-4o-mini"
		}
		return m
	}
}

func withTarget(base, target string) string {
	if target == "" || target == "en" {
		return base
	}
	return base + "#" + target
}
