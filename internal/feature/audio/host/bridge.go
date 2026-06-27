package host

import (
	"path/filepath"

	"translation-overlay/internal/platform/domain"
	"translation-overlay/internal/platform/port"
)

type Host struct {
	repo port.SettingsRepository
}

func New(repo port.SettingsRepository) *Host {
	h := &Host{repo: repo}
	desktopOverlay.load = h.readSettingsFromDisk
	desktopOverlay.logDir = filepath.Dir(repo.Path())
	return h
}

func (h *Host) readSettingsFromDisk() settingsFile {
	st, err := h.repo.Load()
	if err != nil {
		return defaultSettingsFile()
	}
	return normalizeSettings(audioFromDomain(st))
}

func audioFromDomain(st domain.Settings) settingsFile {
	return settingsFile{
		AudioSettings:     st.Audio,
		OpenAIAPIKey:      st.OpenAIAPIKey,
		OpenAIBaseURL:     st.OpenAIBaseURL,
		OpenRouterAPIKey:  st.OpenRouterAPIKey,
		OpenRouterBaseURL: st.OpenRouterBaseURL,
		DashScopeAPIKey:   st.DashScopeAPIKey,
		DashScopeBaseURL:  st.DashScopeBaseURL,
	}
}

func applyAudioToDomain(st *domain.Settings, s settingsFile) {
	st.OpenAIAPIKey = s.OpenAIAPIKey
	st.OpenAIBaseURL = s.OpenAIBaseURL
	st.OpenRouterAPIKey = s.OpenRouterAPIKey
	st.OpenRouterBaseURL = s.OpenRouterBaseURL
	st.DashScopeAPIKey = s.DashScopeAPIKey
	st.DashScopeBaseURL = s.DashScopeBaseURL
	st.Audio = s.AudioSettings
}
