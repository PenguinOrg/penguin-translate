package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"translation-overlay/internal/composition"
	audiohost "translation-overlay/internal/feature/audio/host"
	mictranslatehost "translation-overlay/internal/feature/mictranslate/host"
	"translation-overlay/internal/platform/domain"
)

func settingsResponse(app *composition.App, st domain.Settings) map[string]any {
	return map[string]any{
		"openai_api_key":            st.OpenAIAPIKey,
		"openrouter_api_key":        st.OpenRouterAPIKey,
		"openai_key_configured":     strings.TrimSpace(st.OpenAIAPIKey) != "",
		"openrouter_key_configured": strings.TrimSpace(st.OpenRouterAPIKey) != "",
		"openai_base_url":           st.OpenAIBaseURL,
		"openrouter_base_url":       st.OpenRouterBaseURL,
		"skip_words":                append([]string{}, st.Window.SkipWords...),
		"practice":                  app.MicTranslate.PublicSettings(st),
		"audio":                     audiohost.PublicSettings(st),
	}
}

type unifiedSettingsPost struct {
	OpenAIAPIKey        string          `json:"openai_api_key"`
	RemoveOpenAIKey     bool            `json:"remove_openai_key"`
	OpenAIBaseURL       string          `json:"openai_base_url"`
	OpenRouterAPIKey    string          `json:"openrouter_api_key"`
	RemoveOpenRouterKey bool            `json:"remove_openrouter_key"`
	OpenRouterBaseURL   string          `json:"openrouter_base_url"`
	SkipWords           []string        `json:"skip_words"`
	Practice            json.RawMessage `json:"practice"`
	Audio               json.RawMessage `json:"audio"`
}

func handleSettings(app *composition.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			st, err := app.SettingsRepo.Load()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeSettingsJSON(w, settingsResponse(app, st))
		case http.MethodPost:
			const max = 64 << 10
			body, err := io.ReadAll(io.LimitReader(r.Body, max+1))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(body) > max {
				http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
				return
			}
			var in unifiedSettingsPost
			if err := json.Unmarshal(body, &in); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			st, err := app.SettingsRepo.Load()
			if err != nil {
				st = domain.DefaultSettings("http://127.0.0.1:8745")
			}

			if in.RemoveOpenAIKey {
				st.OpenAIAPIKey = ""
			} else if k := strings.TrimSpace(in.OpenAIAPIKey); k != "" {
				st.OpenAIAPIKey = k
			}
			if in.RemoveOpenRouterKey {
				st.OpenRouterAPIKey = ""
			} else if k := strings.TrimSpace(in.OpenRouterAPIKey); k != "" {
				st.OpenRouterAPIKey = k
			}
			if bodyHasTopKey(body, "openai_base_url") {
				st.OpenAIBaseURL = strings.TrimSpace(in.OpenAIBaseURL)
			}
			if bodyHasTopKey(body, "openrouter_base_url") {
				st.OpenRouterBaseURL = strings.TrimSpace(in.OpenRouterBaseURL)
			}
			if in.SkipWords != nil {
				st.Window.SkipWords = normalizeSkipWords(in.SkipWords)
			}

			if hasSection(in.Practice) {
				if st, err = mictranslatehost.ApplySettingsPatch(st, in.Practice); err != nil {
					http.Error(w, "invalid mic-translate settings", http.StatusBadRequest)
					return
				}
			}
			audioTouched := hasSection(in.Audio)
			if audioTouched {
				if st, err = audiohost.ApplySettingsPatch(st, in.Audio); err != nil {
					http.Error(w, "invalid audio settings", http.StatusBadRequest)
					return
				}
			}

			if err := app.SettingsRepo.Save(st); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if audioTouched {
				audiohost.SyncOverlayLayout(st)
			}
			writeSettingsJSON(w, settingsResponse(app, st))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func hasSection(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

func bodyHasTopKey(body []byte, key string) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return false
	}
	_, ok := raw[key]
	return ok
}

func writeSettingsJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func normalizeSkipWords(words []string) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}
