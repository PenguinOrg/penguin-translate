//go:build windows

package host

import (
	"log"
	"net/http"
	"time"

	"translation-overlay/internal/feature/window/infra/cache"
	"translation-overlay/internal/feature/window/infra/ocr"
	"translation-overlay/internal/feature/window/infra/translate"
	"translation-overlay/internal/platform/lifecycle"
)

func (h *Host) MountRoutes(mux *http.ServeMux) {
	h.once.Do(func() {
		st, err := h.repo.Load()
		if err != nil {
			return
		}
		ocrDir, err := ocr.ResolveDir(st.Window.OCRDir)
		if err != nil {
			return
		}
		engine, err := ocr.NewEngine(ocrDir)
		if err != nil {
			return
		}
		cacheStore, err := cache.Open(translate.CacheKey(st))
		if err != nil {
			engine.Close()
			return
		}
		srv := newServer(h.repo, st, engine, cacheStore)
		srv.InitOverlays(st.Window)
		srv.RegisterRoutes(mux)
		go func() {
			for i := 0; i < 120; i++ {
				srv.ResumeSessionIfNeeded()
				if srv.Runner().Running() {
					return
				}
				time.Sleep(500 * time.Millisecond)
			}
		}()
		lifecycle.Register(func() { srv.Shutdown() })
		log.Print("window-translate: OCR routes mounted (desktop overlay when enabled in settings)")
	})
}
