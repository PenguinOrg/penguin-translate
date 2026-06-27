//go:build windows

package host

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	windows "golang.org/x/sys/windows"
	"translation-overlay/internal/feature/window/infra/cache"
	"translation-overlay/internal/feature/window/infra/ocr"
	"translation-overlay/internal/feature/window/infra/overlay"
	"translation-overlay/internal/feature/window/infra/runner"
	"translation-overlay/internal/feature/window/infra/translate"
	"translation-overlay/internal/feature/window/infra/win"
	"translation-overlay/internal/platform/domain"
	"translation-overlay/internal/platform/port"
)

type Server struct {
	repo      port.SettingsRepository
	st        domain.Settings
	engine    ocr.Recognizer
	store     *cache.Store
	runner    *runner.Runner
	pres      *overlay.IPCPresenter
	hotkeyWin *HotkeyWindow

	mu      sync.Mutex
	windows []win.Window
	subs    map[chan runner.Update]struct{}
	last    runner.Update
}

func newServer(repo port.SettingsRepository, st domain.Settings, engine ocr.Recognizer, store *cache.Store) *Server {
	tr := translate.NewFromSettings(st)
	ws, _ := win.ListVisible()
	target := win.Window{}
	w := st.Window
	if w.WindowHWND != 0 {
		target.HWND = windows.Handle(w.WindowHWND)
		target.Title = w.WindowTitle
		for _, ww := range ws {
			if uint64(ww.HWND) == w.WindowHWND {
				target = ww
				break
			}
		}
	} else if len(ws) > 0 {
		target = ws[0]
	}
	r := runner.New(repo, w, engine, store, tr, target)
	s := &Server{
		repo:    repo,
		st:      st,
		engine:  engine,
		store:   store,
		runner:  r,
		windows: ws,
		subs:    make(map[chan runner.Update]struct{}),
	}
	r.SetOnUpdate(s.broadcast)
	return s
}

func (s *Server) InitOverlays(w domain.WindowSettings) {
	NormalizeHotkey(&w)
	pres, hk := InitOverlays(s.runner, w)
	s.pres = pres
	s.hotkeyWin = hk
	if pres == nil {
		if w.OverlayEnabled || w.VROverlayEnabled {
			log.Print("window-translate: overlay flags set but overlay sidecar unavailable")
		}
		return
	}
	log.Print("window-translate: overlay sidecar ready")
}

func (s *Server) ensureOverlays(w domain.WindowSettings) {
	if (w.OverlayEnabled || w.VROverlayEnabled) && s.pres == nil {
		s.InitOverlays(w)
	}
}

func (s *Server) broadcast(u runner.Update) {
	s.mu.Lock()
	s.last = u
	for ch := range s.subs {
		select {
		case ch <- u:
		default:

			select {
			case <-ch:
			default:
			}
			select {
			case ch <- u:
			default:
			}
		}
	}
	s.mu.Unlock()
}

func (s *Server) handleWindows(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	ws, err := win.ListVisible()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.mu.Lock()
	s.windows = ws
	s.mu.Unlock()
	type row struct {
		Index   int    `json:"index"`
		Title   string `json:"title"`
		Process string `json:"process"`
		HWND    uint64 `json:"hwnd"`
		Label   string `json:"label"`
	}
	out := make([]row, len(ws))
	for i, ww := range ws {
		out[i] = row{
			Index: i, Title: ww.Title, Process: ww.ProcessName,
			HWND: uint64(ww.HWND), Label: win.FormatLabel(ww, i),
		}
	}
	writeJSON(w, out)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if st, err := s.repo.Load(); err == nil {
			s.st = st
			writeJSON(w, st.Window)
			return
		}
		writeJSON(w, s.st.Window)
	case http.MethodPost:
		var win_ domain.WindowSettings
		if err := json.NewDecoder(r.Body).Decode(&win_); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		domain.NormalizeWindowSettings(&win_)
		st, err := s.repo.Load()
		if err != nil {
			st = s.st
		}
		win_.SessionActive = st.Window.SessionActive
		if len(win_.SkipWords) == 0 {
			win_.SkipWords = st.Window.SkipWords
		}
		st.Window = win_
		_ = s.repo.Save(st)
		if reloaded, err := s.repo.Load(); err == nil {
			st = reloaded
		}
		s.st = st
		cfg := st.Window
		s.store.SetModel(translate.CacheKey(st))
		s.runner.UpdateConfig(cfg)
		s.mu.Lock()
		found := false
		for _, ww := range s.windows {
			if uint64(ww.HWND) == cfg.WindowHWND {
				s.runner.SetTarget(ww)
				found = true
				break
			}
		}
		if !found && cfg.WindowHWND != 0 {
			s.runner.SetTarget(win.Window{
				HWND:        windows.Handle(cfg.WindowHWND),
				Title:       cfg.WindowTitle,
				ProcessName: cfg.WindowProcessName,
			})
		}
		s.mu.Unlock()
		tr := translate.NewFromSettings(st)
		s.runner.SetTranslator(tr)
		NormalizeHotkey(&cfg)
		s.ensureOverlays(cfg)
		ApplyHotkey(s.hotkeyWin, s.runner, cfg)
		ApplyVRConfig(s.pres, cfg)
		writeJSON(w, map[string]string{"ok": "saved"})
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	}
}

func (s *Server) setSessionActive(active bool) {
	st, err := s.repo.Load()
	if err != nil {
		s.st.Window.SessionActive = active
		return
	}
	st.Window.SessionActive = active
	_ = s.repo.Save(st)
	if reloaded, err := s.repo.Load(); err == nil {
		s.st = reloaded
	}
}

func (s *Server) ResumeSessionIfNeeded() {
	st := s.st
	if loaded, err := s.repo.Load(); err == nil {
		st = loaded
		s.st = loaded
	}
	w := st.Window
	if !w.SessionActive || s.runner.Running() {
		return
	}
	if strings.EqualFold(w.TranslateBackend, "nllb") || strings.EqualFold(w.TranslateBackend, "local") {
		if err := translate.CheckEngine(w.NLLBBaseURL); err != nil {
			log.Printf("window-translate: defer session resume: %v", err)
			return
		}
	}
	s.ensureOverlays(w)
	ApplyHotkey(s.hotkeyWin, s.runner, w)
	s.store.SetModel(translate.CacheKey(st))
	s.runner.UpdateConfig(w)
	s.runner.SetTranslator(translate.NewFromSettings(st))
	if err := s.runner.Start(); err != nil {
		log.Printf("window-translate: session resume failed: %v", err)
		return
	}
	log.Print("window-translate: resumed active session from last run")
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	st := s.st
	if loaded, err := s.repo.Load(); err == nil {
		st = loaded
		s.st = loaded
	}
	cfg := st.Window
	if strings.EqualFold(cfg.TranslateBackend, "nllb") || strings.EqualFold(cfg.TranslateBackend, "local") {
		if err := translate.CheckEngine(cfg.NLLBBaseURL); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
	}
	s.ensureOverlays(cfg)
	ApplyHotkey(s.hotkeyWin, s.runner, cfg)
	s.store.SetModel(translate.CacheKey(st))
	s.runner.UpdateConfig(cfg)
	s.runner.SetTranslator(translate.NewFromSettings(st))
	if err := s.runner.Start(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.setSessionActive(true)
	writeJSON(w, map[string]bool{"running": true})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.runner.Stop()
	s.setSessionActive(false)
	writeJSON(w, map[string]bool{"running": false})
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	last := s.last
	s.mu.Unlock()
	type payload struct {
		runner.Update
		Running bool `json:"running"`
		Paused  bool `json:"paused"`
		Waiting bool `json:"waiting"`
	}
	writeJSON(w, payload{
		Update:  last,
		Running: s.runner.Running(),
		Paused:  s.runner.Paused(),
		Waiting: s.runner.Waiting(),
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	if _, err := fmt.Fprintf(w, ": connected\n\n"); err != nil {
		return
	}
	if err := rc.Flush(); err != nil {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch := make(chan runner.Update, 16)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	last := s.last
	s.mu.Unlock()
	if last.OCR != "" || last.Translation != "" || last.Err != "" || last.Status != "" {
		select {
		case ch <- last:
		default:
		}
	}

	ctx := r.Context()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
		close(ch)
	}()

	fmt.Fprintf(w, "event: status\ndata: %s\n\n", statusPayload(s.runner.Running()))
	_ = rc.Flush()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-keepalive.C:
			if _, err := fmt.Fprintf(w, ": keepalive\n\n"); err != nil {
				return
			}
			_ = rc.Flush()
		case u, ok := <-ch:
			if !ok {
				return
			}
			b, _ := json.Marshal(u)
			if _, err := fmt.Fprintf(w, "event: update\ndata: %s\n\n", b); err != nil {
				return
			}
			_ = rc.Flush()
		}
	}
}

func statusPayload(running bool) string {
	b, _ := json.Marshal(map[string]bool{"running": running})
	return string(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func (s *Server) Runner() *runner.Runner { return s.runner }

func (s *Server) Shutdown() {
	if s == nil {
		return
	}
	s.runner.Stop()
	if s.pres != nil {
		s.pres.Close()
		s.pres = nil
	}
	if s.hotkeyWin != nil {
		s.hotkeyWin.Close()
		s.hotkeyWin = nil
	}
}
