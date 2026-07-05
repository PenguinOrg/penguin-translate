package livetranslate

import (
	"strings"

	"translation-overlay/internal/platform/port"
)

// Host wires the live-translate bridge to persisted settings (the Gemini API key
// and model live on domain.Settings, read fresh per session start). Its
// HandleMicWS is registered on the native loopback sidecar, not the app mux.
type Host struct {
	repo port.SettingsRepository
}

func New(repo port.SettingsRepository) *Host { return &Host{repo: repo} }

// creds reads the Gemini key, live model, and default echo flag from disk. The
// key stays server-side; it is never sent to the browser.
func (h *Host) creds() (key, model string, echo bool) {
	model = "gemini-3.5-live-translate-preview"
	st, err := h.repo.Load()
	if err != nil {
		return "", model, false
	}
	if m := strings.TrimSpace(st.MicTranslate.LiveModel); m != "" {
		model = m
	}
	return strings.TrimSpace(st.GeminiAPIKey), model, st.MicTranslate.LiveEcho
}
