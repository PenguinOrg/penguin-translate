package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"translation-overlay/internal/composition"
	audiohost "translation-overlay/internal/feature/audio/host"
	mictranslatehost "translation-overlay/internal/feature/mictranslate/host"
	"translation-overlay/internal/platform/domain"
)

// API keys are write-only over HTTP: GET exposes only the *_key_configured flags.
func settingsResponse(app *composition.App, st domain.Settings) map[string]any {
	return map[string]any{
		"openai_key_configured":     strings.TrimSpace(st.OpenAIAPIKey) != "",
		"openrouter_key_configured": strings.TrimSpace(st.OpenRouterAPIKey) != "",
		"dashscope_key_configured":  strings.TrimSpace(st.DashScopeAPIKey) != "",
		"deepgram_key_configured":   strings.TrimSpace(st.DeepgramAPIKey) != "",
		"azure_key_configured":      strings.TrimSpace(st.AzureSpeechKey) != "",
		"openai_base_url":           st.OpenAIBaseURL,
		"openrouter_base_url":       st.OpenRouterBaseURL,
		"dashscope_base_url":        st.DashScopeBaseURL,
		"deepgram_base_url":         st.DeepgramBaseURL,
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
	DashScopeAPIKey     string          `json:"dashscope_api_key"`
	RemoveDashScopeKey  bool            `json:"remove_dashscope_key"`
	DashScopeBaseURL    string          `json:"dashscope_base_url"`
	DeepgramAPIKey      string          `json:"deepgram_api_key"`
	RemoveDeepgramKey   bool            `json:"remove_deepgram_key"`
	DeepgramBaseURL     string          `json:"deepgram_base_url"`
	AzureSpeechKey      string          `json:"azure_speech_key"`
	RemoveAzureKey      bool            `json:"remove_azure_key"`
	SkipWords           []string        `json:"skip_words"`
	Practice            json.RawMessage `json:"practice"`
	Audio               json.RawMessage `json:"audio"`
}

var (
	errInvalidPractice = errors.New("invalid mic-translate settings")
	errInvalidAudio    = errors.New("invalid audio settings")
)

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
			audioTouched := hasSection(in.Audio)
			st, err := app.SettingsRepo.Update(func(st *domain.Settings) error {
				return applyUnifiedSettingsPost(st, in, body)
			})
			if err != nil {
				code := http.StatusInternalServerError
				if errors.Is(err, errInvalidPractice) || errors.Is(err, errInvalidAudio) {
					code = http.StatusBadRequest
				}
				http.Error(w, err.Error(), code)
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

func applyUnifiedSettingsPost(st *domain.Settings, in unifiedSettingsPost, body []byte) error {
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
	if in.RemoveDashScopeKey {
		st.DashScopeAPIKey = ""
	} else if k := strings.TrimSpace(in.DashScopeAPIKey); k != "" {
		st.DashScopeAPIKey = k
	}
	if in.RemoveDeepgramKey {
		st.DeepgramAPIKey = ""
	} else if k := strings.TrimSpace(in.DeepgramAPIKey); k != "" {
		st.DeepgramAPIKey = k
	}
	if in.RemoveAzureKey {
		st.AzureSpeechKey = ""
	} else if k := strings.TrimSpace(in.AzureSpeechKey); k != "" {
		st.AzureSpeechKey = k
	}
	if bodyHasTopKey(body, "openai_base_url") {
		st.OpenAIBaseURL = strings.TrimSpace(in.OpenAIBaseURL)
	}
	if bodyHasTopKey(body, "openrouter_base_url") {
		st.OpenRouterBaseURL = strings.TrimSpace(in.OpenRouterBaseURL)
	}
	if bodyHasTopKey(body, "dashscope_base_url") {
		st.DashScopeBaseURL = strings.TrimSpace(in.DashScopeBaseURL)
	}
	if bodyHasTopKey(body, "deepgram_base_url") {
		st.DeepgramBaseURL = strings.TrimSpace(in.DeepgramBaseURL)
	}
	if in.SkipWords != nil {
		st.Window.SkipWords = normalizeSkipWords(in.SkipWords)
	}

	if hasSection(in.Practice) {
		next, err := mictranslatehost.ApplySettingsPatch(*st, in.Practice)
		if err != nil {
			return errInvalidPractice
		}
		*st = next
	}
	if hasSection(in.Audio) {
		next, err := audiohost.ApplySettingsPatch(*st, in.Audio)
		if err != nil {
			return errInvalidAudio
		}
		*st = next
	}
	return nil
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
