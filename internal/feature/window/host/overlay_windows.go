//go:build windows

package host

import (
	"translation-overlay/internal/feature/window/infra/overlay"
	"translation-overlay/internal/feature/window/infra/runner"
	"translation-overlay/internal/platform/domain"
)

func InitOverlays(r *runner.Runner, w domain.WindowSettings) (*overlay.IPCPresenter, *HotkeyWindow) {
	if !w.OverlayEnabled && !w.VROverlayEnabled {
		return nil, nil
	}
	pres := overlay.NewIPCPresenter(w.OverlayEnabled, w.VROverlayEnabled, w.VRHUDWidthM, w.VRHUDDistanceM)
	r.SetPresenter(pres)
	pres.Configure(w.OverlayEnabled, w.VROverlayEnabled, w.VRHUDWidthM, w.VRHUDDistanceM)
	hk := NewHotkeyWindow()
	return pres, hk
}

func ApplyVRConfig(pres *overlay.IPCPresenter, w domain.WindowSettings) {
	if pres == nil {
		return
	}
	pres.Configure(w.OverlayEnabled, w.VROverlayEnabled, w.VRHUDWidthM, w.VRHUDDistanceM)
}
