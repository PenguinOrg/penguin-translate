//go:build windows

package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	windows "golang.org/x/sys/windows"
	"translation-overlay/internal/feature/window/infra/cache"
	"translation-overlay/internal/feature/window/infra/latency"
	"translation-overlay/internal/feature/window/infra/ocr"
	"translation-overlay/internal/feature/window/infra/overlay"
	"translation-overlay/internal/feature/window/infra/translate"
	"translation-overlay/internal/feature/window/infra/win"
	"translation-overlay/internal/platform/domain"
	"translation-overlay/internal/platform/port"
)

const captureIntervalMS = 200

const targetResolveInterval = 2000 * time.Millisecond

type Update struct {
	Status      string `json:"status,omitempty"`
	OCR         string `json:"ocr,omitempty"`
	Translation string `json:"translation,omitempty"`
	Cached      bool   `json:"cached,omitempty"`
	Partial     bool   `json:"partial,omitempty"`
	Paused      bool   `json:"paused,omitempty"`
	Err         string `json:"err,omitempty"`
}

type ocrFrame struct {
	result      *ocr.Result
	text        string
	placement   win.Placement
	frameHash   uint64
	frameID     uint64
	capUS       int64
	framehashUS int64
	ocrUS       int64
}

type Runner struct {
	repo port.SettingsRepository
	cfg  domain.WindowSettings

	engine  ocr.Recognizer
	store   *cache.Store
	tr      translate.Translator
	display *overlay.Display

	target win.Window

	mu              sync.Mutex
	running         bool
	paused          bool
	waiting         bool
	notReady        bool
	lastResolve     time.Time
	cancel          context.CancelFunc
	lastOCR         string
	lastTranslation string
	lastFrameHash   uint64
	lastLabels      []overlay.Label
	lastResults     []translate.LineResult

	lastOCRLines []ocr.Line
	lastSrc      []string
	lastFrameID  uint64

	trGen       uint64
	trInFlight  bool
	trActiveGen uint64
	lastTrStart time.Time

	ocrBusy bool

	onUpdate func(Update)
}

func New(repo port.SettingsRepository, w domain.WindowSettings, engine ocr.Recognizer, store *cache.Store, tr translate.Translator, target win.Window) *Runner {
	translate.SetSkipWords(w.SkipWords)
	return &Runner{
		repo:   repo,
		cfg:    w,
		engine: engine,
		store:  store,
		tr:     tr,
		target: target,
	}
}

func (r *Runner) SetPresenter(p overlay.Presenter) {
	r.display = overlay.NewDisplay(p)
}

func (r *Runner) present(target windows.Handle, place win.Placement, labels []overlay.Label) {
	r.presentFrame(target, place, labels, 0)
}

func (r *Runner) presentFrame(target windows.Handle, place win.Placement, labels []overlay.Label, frameID uint64) {
	if r.display != nil {
		r.display.PresentFrame(target, place, labels, frameID)
	}
}

func (r *Runner) SetOnUpdate(fn func(Update)) {
	r.onUpdate = fn
}

func (r *Runner) SetTarget(w win.Window) {
	r.mu.Lock()
	r.target = w
	r.cfg.WindowHWND = uint64(w.HWND)
	r.cfg.WindowTitle = w.Title
	r.mu.Unlock()
	r.saveConfigLocked()
}

func (r *Runner) acquireOrArm() {
	r.mu.Lock()
	procName := strings.TrimSpace(r.cfg.WindowProcessName)
	hwnd := r.target.HWND
	r.mu.Unlock()

	if procName == "" {
		return
	}
	if w, ok := win.FindByProcessName(procName); ok {
		r.setTargetResolved(w)
		return
	}
	if win.IsWindow(hwnd) {
		return
	}
	r.mu.Lock()
	r.waiting = true
	r.lastResolve = time.Now()
	r.clearOverlayCacheLocked()
	r.mu.Unlock()
	r.emit(Update{Status: "Waiting for " + procName + "… (launch the app)"})
}

func (r *Runner) setTargetResolved(w win.Window) {
	r.mu.Lock()
	r.target = w
	r.cfg.WindowHWND = uint64(w.HWND)
	r.cfg.WindowTitle = w.Title
	r.waiting = false
	r.notReady = false
	r.lastResolve = time.Now()
	r.mu.Unlock()
	r.saveConfigLocked()
}

func (r *Runner) holdNotReady(status string) {
	r.mu.Lock()
	changed := !r.notReady
	r.notReady = true
	if changed {
		r.clearOverlayCacheLocked()
	}
	r.mu.Unlock()
	if changed {
		r.syncOverlayVisible()
		r.emit(Update{Status: status})
	}
}

func (r *Runner) clearNotReady() {
	r.mu.Lock()
	r.notReady = false
	r.mu.Unlock()
}

func (r *Runner) UpdateConfig(w domain.WindowSettings) {
	r.mu.Lock()
	r.cfg = w
	r.mu.Unlock()
	translate.SetSkipWords(w.SkipWords)
	r.saveConfigLocked()
	r.syncOverlayVisible()
}

func (r *Runner) saveConfigLocked() {
	if r.repo == nil {
		return
	}
	r.mu.Lock()
	w := r.cfg
	r.mu.Unlock()
	st, err := r.repo.Load()
	if err != nil {
		return
	}
	w.SessionActive = st.Window.SessionActive
	st.Window = w
	_ = r.repo.Save(st)
}

func (r *Runner) SetTranslator(tr translate.Translator) {
	r.mu.Lock()
	r.tr = tr
	r.mu.Unlock()
}

func (r *Runner) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

func (r *Runner) Paused() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.paused
}

func (r *Runner) Waiting() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.waiting
}

func (r *Runner) overlayVisible() bool {
	return r.display != nil && r.display.Visible()
}

func (r *Runner) syncOverlayVisible() {
	r.mu.Lock()
	visible := r.running && !r.paused && !r.waiting && !r.notReady &&
		(r.cfg.OverlayEnabled || r.cfg.VROverlayEnabled)
	r.mu.Unlock()
	if r.display != nil {
		r.display.SetVisible(visible)
	}
}

func (r *Runner) clearOverlayCacheLocked() {
	r.lastLabels = nil
	r.lastResults = nil
	r.lastOCRLines = nil
	r.lastSrc = nil
}

func (r *Runner) TogglePaused() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	r.paused = !r.paused
	paused := r.paused
	hotkey := r.cfg.Hotkey
	if paused {
		r.clearOverlayCacheLocked()
	}
	r.mu.Unlock()

	r.syncOverlayVisible()
	status := "Translation ON — overlay active"
	if paused {
		status = "Translation OFF (paused)"
		if hk := strings.TrimSpace(hotkey); hk != "" && !strings.EqualFold(hk, "none") {
			status += " — press " + hk + " to resume"
		}
	}
	r.emit(Update{Status: status, Paused: paused, OCR: r.lastOCRText(), Translation: r.lastTranslationSnapshot()})
}

func (r *Runner) lastTranslationSnapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastTranslation
}

func (r *Runner) Start() error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.running = true
	r.lastOCR = ""
	r.lastTranslation = ""
	r.lastFrameHash = 0
	r.lastLabels = nil
	r.lastResults = nil
	r.trGen = 0
	r.trInFlight = false
	r.trActiveGen = 0
	r.paused = false
	r.cancel = cancel
	r.mu.Unlock()

	r.acquireOrArm()
	r.syncOverlayVisible()

	go r.loop(ctx)
	go func() {
		time.Sleep(50 * time.Millisecond)
		r.runCaptureTick()
	}()
	return nil
}

func (r *Runner) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	r.cancel()
	r.running = false
	r.paused = false
	r.waiting = false
	r.clearOverlayCacheLocked()
	r.mu.Unlock()
	r.syncOverlayVisible()
}

func (r *Runner) emit(u Update) {
	if r.onUpdate != nil {
		r.onUpdate(u)
	}
}

func (r *Runner) loop(ctx context.Context) {
	interval := time.Duration(captureIntervalMS) * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runCaptureTick()
		}
	}
}

func (r *Runner) runCaptureTick() {
	tickStart := time.Now()
	r.mu.Lock()
	target := r.target
	paused := r.paused
	running := r.running
	ocrBusy := r.ocrBusy
	procName := strings.TrimSpace(r.cfg.WindowProcessName)
	lastLabels := append([]overlay.Label(nil), r.lastLabels...)
	r.mu.Unlock()

	r.syncOverlayVisible()

	if !running {
		return
	}
	if paused {
		r.emit(Update{
			Status:      "Translation OFF (paused) — press hotkey to resume",
			Paused:      true,
			OCR:         r.lastOCRText(),
			Translation: r.lastTranslationSnapshot(),
		})
		return
	}

	if !win.IsWindow(target.HWND) {
		if procName == "" {
			r.emit(Update{Err: "no target window selected"})
			return
		}
		r.mu.Lock()
		due := time.Since(r.lastResolve) >= targetResolveInterval
		if due {
			r.lastResolve = time.Now()
		}
		justEntered := !r.waiting
		if justEntered {
			r.waiting = true
			r.notReady = false
			r.clearOverlayCacheLocked()
		}
		r.mu.Unlock()
		if justEntered {
			r.syncOverlayVisible()
		}

		if !due {
			return
		}
		w, ok := win.FindByProcessName(procName)
		if !ok {
			r.emit(Update{Status: "Waiting for " + procName + "… (launch the app)"})
			return
		}
		r.setTargetResolved(w)
		target = w
	}

	if win.IsIconic(target.HWND) {
		r.holdNotReady(procName + " is minimized — waiting to capture…")
		return
	}

	tCap := time.Now()
	frame, err := win.CaptureHWND(target.HWND)
	capUS := latency.US(tCap)
	capMs := capUS / 1000
	if err != nil {
		if !win.IsWindow(target.HWND) {
			r.mu.Lock()
			r.waiting = true
			r.notReady = false
			r.lastResolve = time.Now()
			r.clearOverlayCacheLocked()
			r.mu.Unlock()
			r.syncOverlayVisible()
			r.emit(Update{Status: "Waiting for " + procName + "… (window closed)"})
			return
		}
		if errors.Is(err, win.ErrWindowNotReady) {
			r.holdNotReady("Waiting for " + procName + " to be ready…")
			return
		}
		r.emit(Update{Err: "capture: " + err.Error()})
		r.present(target.HWND, win.Placement{}, nil)
		return
	}
	r.clearNotReady()

	tHash := time.Now()
	hash := win.FrameHash(frame.Pixels, frame.Width, frame.Height)
	framehashUS := latency.US(tHash)
	r.mu.Lock()
	frameUnchanged := hash != 0 && hash == r.lastFrameHash
	lastEN := r.lastTranslation
	frameHash := hash
	r.mu.Unlock()

	if frameUnchanged {
		r.present(target.HWND, frame.Placement, lastLabels)
		r.emit(Update{
			Status: fmt.Sprintf("Captured %d×%d — unchanged (skipped OCR) · %dms cap",
				frame.Width, frame.Height, capMs),
			Translation: lastEN,
			OCR:         r.lastOCRText(),
		})
		r.maybeRetryTranslation(target.HWND, frame.Placement)
		return
	}

	if ocrBusy {
		r.present(target.HWND, frame.Placement, lastLabels)
		r.maybeRetryTranslation(target.HWND, frame.Placement)
		return
	}

	hwnd := target.HWND
	place := frame.Placement
	pixels := append([]byte(nil), frame.Pixels...)
	width := frame.Width
	height := frame.Height

	r.mu.Lock()
	r.ocrBusy = true
	r.mu.Unlock()

	frameID := latency.NextFrameID()

	go func() {
		defer func() {
			r.mu.Lock()
			r.ocrBusy = false
			r.mu.Unlock()
		}()

		tOCR := time.Now()
		result, err := r.engine.RecognizeResult(pixels, width, height)
		ocrUS := latency.US(tOCR)
		if err != nil {
			r.emit(Update{Status: fmt.Sprintf("Captured %d×%d", width, height), Err: "ocr: " + err.Error()})
			r.present(hwnd, place, nil)
			return
		}

		text := strings.TrimSpace(result.FullText)
		if text == "" {
			r.emit(Update{
				Status: fmt.Sprintf("Captured %d×%d — no text (try a window with visible JP/ZH text)", width, height),
			})
			r.present(hwnd, place, nil)
			return
		}

		r.finishOCR(tickStart, ocrFrame{
			result:      result,
			text:        text,
			placement:   place,
			frameHash:   frameHash,
			frameID:     frameID,
			capUS:       capUS,
			framehashUS: framehashUS,
			ocrUS:       ocrUS,
		}, hwnd, width, height)
	}()
}

func (r *Runner) finishOCR(tickStart time.Time, fr ocrFrame, hwnd windows.Handle, width, height int) {
	r.mu.Lock()
	cfg := r.cfg
	store := r.store
	textUnchanged := fr.text == r.lastOCR
	if !textUnchanged {
		r.lastOCR = fr.text
		r.trGen++
	}
	gen := r.trGen
	r.lastFrameHash = fr.frameHash
	r.mu.Unlock()

	latency.RecordRunner(fr.frameID, map[string]int64{
		"capture":   fr.capUS,
		"framehash": fr.framehashUS,
		"ocr":       fr.ocrUS,
	})

	result := fr.result
	text := fr.text
	place := fr.placement

	var labels []overlay.Label
	var known []translate.LineResult

	if len(result.Lines) == 0 {
		r.present(hwnd, place, nil)
		r.emit(Update{
			Status: fmt.Sprintf("Captured %d×%d — no lines", width, height),
			OCR:    text,
		})
		return
	}

	tResolve := time.Now()
	src := make([]string, len(result.Lines))
	for i, ln := range result.Lines {
		src[i] = ln.Text
	}

	r.mu.Lock()
	if textUnchanged && len(r.lastResults) == len(result.Lines) {
		known = append([]translate.LineResult(nil), r.lastResults...)
	} else {
		known, _ = translate.ResolveKnown(src, store)
	}
	r.mu.Unlock()

	labels = labelsFromOCR(result.Lines, known, cfg.OverlayShowSource)
	fullEN := joinTranslated(result.Lines, known, cfg.OverlayShowSource)
	resolveUS := latency.US(tResolve)
	latency.RecordRunner(fr.frameID, map[string]int64{"resolve": resolveUS})

	r.mu.Lock()
	r.lastLabels = append([]overlay.Label(nil), labels...)
	r.lastResults = append([]translate.LineResult(nil), known...)
	r.lastTranslation = fullEN
	r.lastOCRLines = append([]ocr.Line(nil), result.Lines...)
	r.lastSrc = append([]string(nil), src...)
	r.lastFrameID = fr.frameID
	r.mu.Unlock()

	r.presentFrame(hwnd, place, labels, fr.frameID)

	totalMs := time.Since(tickStart).Milliseconds()
	status := fmt.Sprintf("Captured %d×%d — %d lines, %d overlays · %dms (cap %d · ocr %d · tr async)",
		width, height, len(result.Lines), len(labels), totalMs, fr.capUS/1000, fr.ocrUS/1000)
	if textUnchanged {
		status = fmt.Sprintf("Captured %d×%d — %d lines (text unchanged) · %dms (cap %d · ocr %d · tr async)",
			width, height, len(result.Lines), totalMs, fr.capUS/1000, fr.ocrUS/1000)
	}
	r.emit(Update{
		Status:      status,
		OCR:         text,
		Translation: fullEN,
	})

	if translate.PendingTranslation(src, known) {
		r.tryTranslateAsync(gen, fr.frameID, hwnd, place, result.Lines, src, text, cfg)
	}
}

func (r *Runner) maybeRetryTranslation(hwnd windows.Handle, place win.Placement) {
	r.mu.Lock()
	cfg := r.cfg
	gen := r.trGen
	ocrLines := append([]ocr.Line(nil), r.lastOCRLines...)
	src := append([]string(nil), r.lastSrc...)
	text := r.lastOCR
	known := append([]translate.LineResult(nil), r.lastResults...)
	r.mu.Unlock()

	if len(src) == 0 || len(ocrLines) == 0 {
		return
	}
	if !translate.PendingTranslation(src, known) {
		return
	}
	r.mu.Lock()
	frameID := r.lastFrameID
	r.mu.Unlock()
	r.tryTranslateAsync(gen, frameID, hwnd, place, ocrLines, src, text, cfg)
}

func (r *Runner) tryTranslateAsync(
	gen uint64,
	frameID uint64,
	hwnd windows.Handle,
	place win.Placement,
	ocrLines []ocr.Line,
	src []string,
	text string,
	cfg domain.WindowSettings,
) {
	r.mu.Lock()
	if r.trInFlight && r.trActiveGen == gen {
		r.mu.Unlock()
		return
	}
	interval := time.Duration(cfg.PollIntervalMS) * time.Millisecond
	if interval <= 0 {
		interval = 1500 * time.Millisecond
	}
	if time.Since(r.lastTrStart) < interval {
		r.mu.Unlock()
		return
	}
	tr := r.tr
	store := r.store
	r.trInFlight = true
	r.trActiveGen = gen
	r.lastTrStart = time.Now()
	r.mu.Unlock()

	batchOpts := translate.BatchOptsFromConfig(cfg.MaxBatchLines, cfg.MaxBatchChars, cfg.MaxParallelRequests)

	go func() {
		defer func() {
			r.mu.Lock()
			if r.trActiveGen == gen {
				r.trInFlight = false
			}
			r.mu.Unlock()
		}()

		tTr := time.Now()
		onProgress := func(partial []translate.LineResult) {
			if gen != r.currentTrGen() {
				return
			}
			if !r.overlayVisible() {
				return
			}
			labels := labelsFromOCR(ocrLines, partial, cfg.OverlayShowSource)
			r.mu.Lock()
			r.lastLabels = append([]overlay.Label(nil), labels...)
			r.lastResults = append([]translate.LineResult(nil), partial...)
			r.lastTranslation = joinTranslated(ocrLines, partial, cfg.OverlayShowSource)
			r.mu.Unlock()
			r.presentFrame(hwnd, place, labels, frameID)
			nDone, nTotal := countTranslated(ocrLines, partial)
			r.emit(Update{
				Status:      fmt.Sprintf("Translating… %d/%d lines on overlay", nDone, nTotal),
				OCR:         text,
				Translation: joinTranslated(ocrLines, partial, cfg.OverlayShowSource),
				Partial:     true,
			})
		}

		translated, err := translate.TranslateLines(src, cfg.SourceLang, tr, store, batchOpts, onProgress)
		trUS := latency.US(tTr)
		trMs := trUS / 1000
		if gen != r.currentTrGen() {
			return
		}
		if err != nil {
			r.emit(Update{
				Status: fmt.Sprintf("Captured — translate error after %dms", trMs),
				OCR:    text,
				Err:    "translate: " + err.Error(),
			})
			return
		}
		latency.RecordRunner(frameID, map[string]int64{"translate": trUS})

		labels := labelsFromOCR(ocrLines, translated, cfg.OverlayShowSource)
		fullEN := joinTranslated(ocrLines, translated, cfg.OverlayShowSource)
		r.mu.Lock()
		r.lastLabels = append([]overlay.Label(nil), labels...)
		r.lastResults = append([]translate.LineResult(nil), translated...)
		r.lastTranslation = fullEN
		r.mu.Unlock()
		r.presentFrame(hwnd, place, labels, frameID)

		r.emit(Update{
			Status: fmt.Sprintf("Translation done — %d lines · %dms tr",
				len(ocrLines), trMs),
			OCR:         text,
			Translation: fullEN,
		})
	}()
}

func (r *Runner) currentTrGen() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.trGen
}

func (r *Runner) lastOCRText() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastOCR
}

func labelsFromOCR(ocrLines []ocr.Line, translated []translate.LineResult, showSource bool) []overlay.Label {
	var labels []overlay.Label
	for i, ln := range ocrLines {
		if translate.ShouldSkipLine(ln.Text) {
			continue
		}
		var overlayText, overlayRoman string
		outlineOnly := false
		if translate.ShouldSkipTranslation(ln.Text) {
			overlayText = strings.TrimSpace(ln.Text)
		} else if i < len(translated) {
			overlayText = translate.SanitizeForOverlay(translated[i].En)
			overlayRoman = translate.SanitizeRoman(translated[i].Roman)
		}
		if overlayText == "" {
			outlineOnly = true
		} else if showSource && !translate.ShouldSkipTranslation(ln.Text) {
			if src := strings.TrimSpace(ln.Text); src != "" {
				overlayText = src + "\n" + overlayText
			}
		}
		labels = append(labels, overlay.Label{
			Text:        overlayText,
			Roman:       overlayRoman,
			Box:         ln.Box,
			OutlineOnly: outlineOnly,
		})
	}
	return labels
}

func countTranslated(ocrLines []ocr.Line, translated []translate.LineResult) (done, total int) {
	for i, ln := range ocrLines {
		if translate.ShouldSkipLine(ln.Text) {
			continue
		}
		if translate.ShouldSkipTranslation(ln.Text) {
			done++
			total++
			continue
		}
		total++
		if i < len(translated) && strings.TrimSpace(translated[i].En) != "" {
			done++
		}
	}
	return done, total
}

func joinTranslated(lines []ocr.Line, translated []translate.LineResult, showSource bool) string {
	var parts []string
	for i, ln := range lines {
		if translate.ShouldSkipLine(ln.Text) || translate.ShouldSkipTranslation(ln.Text) {
			continue
		}
		en := ""
		if i < len(translated) {
			en = strings.TrimSpace(translated[i].En)
		}
		if en == "" || translate.IsRefusal(en) {
			continue
		}
		if showSource {
			if src := strings.TrimSpace(ln.Text); src != "" {
				en = src + "\n" + en
			}
		}
		parts = append(parts, en)
	}
	return strings.Join(parts, "\n")
}
