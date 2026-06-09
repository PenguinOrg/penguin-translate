package host

import (
	"translation-overlay/internal/platform/domain"
	"translation-overlay/internal/platform/port"
)

type Host struct {
	repo port.SettingsRepository
}

func New(repo port.SettingsRepository) *Host {
	return &Host{repo: repo}
}

func (h *Host) readSettingsFromDisk() settingsFile {
	st, err := h.repo.Load()
	if err != nil {
		return defaultSettingsFile()
	}
	return normalizeSettings(micTranslateFromDomain(st))
}

func (h *Host) appDataDir() (string, error) {
	return dirOf(h.repo.Path()), nil
}

func micTranslateFromDomain(st domain.Settings) settingsFile {
	return settingsFile{
		MicTranslateSettings: st.MicTranslate,
		OpenAIAPIKey:         st.OpenAIAPIKey,
		OpenAIBaseURL:        st.OpenAIBaseURL,
		OpenRouterAPIKey:     st.OpenRouterAPIKey,
		OpenRouterBaseURL:    st.OpenRouterBaseURL,
	}
}

func applyMicTranslateToDomain(st *domain.Settings, s settingsFile) {
	st.OpenAIAPIKey = s.OpenAIAPIKey
	st.OpenAIBaseURL = s.OpenAIBaseURL
	st.OpenRouterAPIKey = s.OpenRouterAPIKey
	st.OpenRouterBaseURL = s.OpenRouterBaseURL
	st.MicTranslate = s.MicTranslateSettings
}

func dirOf(path string) string {
	if path == "" {
		return ""
	}
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return path
}
