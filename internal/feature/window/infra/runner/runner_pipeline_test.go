//go:build windows

package runner

import (
	"strings"
	"sync"
	"testing"
	"time"

	windows "golang.org/x/sys/windows"
	"translation-overlay/internal/feature/window/infra/cache"
	"translation-overlay/internal/feature/window/infra/ocr"
	"translation-overlay/internal/feature/window/infra/overlay"
	"translation-overlay/internal/feature/window/infra/translate"
	"translation-overlay/internal/feature/window/infra/win"
	"translation-overlay/internal/platform/domain"
)

type recPresenter struct {
	mu     sync.Mutex
	labels []overlay.Label
}

func (p *recPresenter) Attach(windows.Handle)      {}
func (p *recPresenter) SetPlacement(win.Placement) {}
func (p *recPresenter) Clear()                     { p.mu.Lock(); p.labels = nil; p.mu.Unlock() }
func (p *recPresenter) Close()                     {}
func (p *recPresenter) SetLabels(l []overlay.Label) {
	p.mu.Lock()
	p.labels = append([]overlay.Label(nil), l...)
	p.mu.Unlock()
}
func (p *recPresenter) snapshot() []overlay.Label {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]overlay.Label(nil), p.labels...)
}

type stubTranslator struct{}

func (stubTranslator) ToTargetLine(text, _ string) (translate.LineResult, error) {
	return translate.LineResult{En: "EN[" + text + "]"}, nil
}
func (stubTranslator) ToTargetBatch(lines []string, _ string) ([]translate.LineResult, error) {
	out := make([]translate.LineResult, len(lines))
	for i, l := range lines {
		out[i] = translate.LineResult{En: "EN[" + l + "]"}
	}
	return out, nil
}

var _ translate.Translator = stubTranslator{}

func TestRunnerPipelineFakeEngineToPresenter(t *testing.T) {
	store, err := cache.Open("test-ocr-pipeline")
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}

	cfg := domain.WindowSettings{OverlayEnabled: true, SourceLang: "ja", MaxBatchLines: 8}
	eng := ocr.NewFakeEngine("これはテストです")
	r := New(nil, cfg, eng, store, stubTranslator{}, win.Window{})

	rec := &recPresenter{}
	r.SetPresenter(rec)

	var mu sync.Mutex
	var lastTranslated Update
	r.SetOnUpdate(func(u Update) {
		if strings.Contains(u.Translation, "EN[") {
			mu.Lock()
			lastTranslated = u
			mu.Unlock()
		}
	})

	r.mu.Lock()
	r.running = true
	r.mu.Unlock()
	r.display.SetVisible(true)

	res, _ := eng.RecognizeResult(nil, 800, 600)
	r.finishOCR(time.Now(), ocrFrame{
		result: res, text: res.FullText, placement: win.Placement{}, frameHash: 1, frameID: 1,
	}, windows.Handle(0), 800, 600)

	deadline := time.Now().Add(3 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		for _, l := range rec.snapshot() {
			if strings.Contains(l.Text, "EN[これはテストです]") {
				got = l.Text
			}
		}
		if got != "" {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if got == "" {
		t.Fatalf("presenter never received the translated label; labels=%+v", rec.snapshot())
	}

	mu.Lock()
	u := lastTranslated
	mu.Unlock()
	if !strings.Contains(u.OCR, "これはテストです") {
		t.Errorf("Update.OCR missing fixture source: %q", u.OCR)
	}
	if !strings.Contains(u.Translation, "EN[これはテストです]") {
		t.Errorf("Update.Translation missing EN: %q", u.Translation)
	}
}
