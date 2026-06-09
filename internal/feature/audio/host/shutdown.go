package host

import (
	"log"

	"translation-overlay/internal/platform/lifecycle"
)

func init() {
	lifecycle.Register(stopProductionOverlays)
}

func stopProductionOverlays() {
	desktopOverlay.stop()
	log.Print("production overlays stopped")
}
