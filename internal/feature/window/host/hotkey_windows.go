//go:build windows

package host

import (
	"log"
	"strings"

	"translation-overlay/internal/feature/window/infra/hotkey"
	"translation-overlay/internal/feature/window/infra/runner"
	"translation-overlay/internal/platform/domain"
)

func ApplyHotkey(hk *HotkeyWindow, r *runner.Runner, w domain.WindowSettings) {
	if hk == nil {
		return
	}
	mods, vk, ok, err := hotkey.Parse(w.Hotkey)
	if err != nil {
		log.Printf("hotkey: %v", err)
		return
	}
	if !ok {
		hk.ClearHotkey()
		log.Print("hotkey disabled")
		return
	}
	hk.SetHotkey(mods, vk, r.TogglePaused)
}

func NormalizeHotkey(w *domain.WindowSettings) {
	if strings.TrimSpace(w.Hotkey) == "" {
		w.Hotkey = "F9"
	}
	h := strings.TrimSpace(w.Hotkey)
	if len(h) == 1 && h[0] >= 'a' && h[0] <= 'z' {
		log.Printf("hotkey: %q is the letter %s (use F9 or Ctrl+%s to avoid typing conflicts)",
			h, strings.ToUpper(h), strings.ToUpper(h))
	}
}
