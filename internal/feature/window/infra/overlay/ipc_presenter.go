//go:build windows

package overlay

import (
	"encoding/json"
	"log"
	"sync"

	windows "golang.org/x/sys/windows"
	"translation-overlay/internal/feature/window/infra/latency"
	"translation-overlay/internal/feature/window/infra/ocr"
	"translation-overlay/internal/feature/window/infra/win"
	"translation-overlay/internal/platform/overlayipc"
)

const wtVROverlayKey = "windowtranslate.overlay"

type IPCPresenter struct {
	proc *overlayipc.Process

	mu             sync.Mutex
	labels         []Label
	place          win.Placement
	frameID        uint64
	desktopEnabled bool
	vrEnabled      bool
	vrWidthM       float64
	vrDistanceM    float64
	vrSeen         bool
	vrOK           bool
	vrDetail       string
}

func NewIPCPresenter(desktopEnabled, vrEnabled bool, vrWidthM, vrDistanceM float64) *IPCPresenter {
	p := &IPCPresenter{
		desktopEnabled: desktopEnabled,
		vrEnabled:      vrEnabled,
		vrWidthM:       vrWidthM,
		vrDistanceM:    vrDistanceM,
	}
	p.proc = overlayipc.New(overlayipc.Config{
		Name: "wt-overlay",
		Env: []string{
			"TO_OVERLAY_VR_KEY=" + wtVROverlayKey,
			"TO_OVERLAY_VR_NAME=Window Translate",
			"TO_OVERLAY_DPI_AWARE=1",
		},
		OnStdout: p.handleStdout,
	})
	return p
}

func (p *IPCPresenter) ensureStarted() {
	if p.proc.IsRunning() {
		return
	}
	if _, err := p.proc.Start(); err != nil {
		log.Printf("wt-overlay start: %v", err)
		return
	}
	p.sendConfigure()
}

func (p *IPCPresenter) sendConfigure() {
	p.mu.Lock()
	desktopEnabled, vrEnabled := p.desktopEnabled, p.vrEnabled
	vrW, vrD := p.vrWidthM, p.vrDistanceM
	p.mu.Unlock()
	_ = p.proc.WriteOp(map[string]any{
		"op":              "configure",
		"desktop_enabled": desktopEnabled,
		"vr_enabled":      vrEnabled,
		"vr_width_m":      vrW,
		"vr_distance_m":   vrD,
	})
}

func (p *IPCPresenter) Configure(desktopEnabled, vrEnabled bool, vrWidthM, vrDistanceM float64) {
	p.mu.Lock()
	p.desktopEnabled = desktopEnabled
	p.vrEnabled = vrEnabled
	p.vrWidthM = vrWidthM
	p.vrDistanceM = vrDistanceM
	p.mu.Unlock()
	if p.proc.IsRunning() {
		p.sendConfigure()
	} else {
		p.ensureStarted()
	}
}

func (p *IPCPresenter) Attach(_ windows.Handle) {}

func (p *IPCPresenter) BeginFrame(id uint64) {
	p.mu.Lock()
	p.frameID = id
	p.mu.Unlock()
}

func (p *IPCPresenter) SetLabels(labels []Label) {
	p.mu.Lock()
	p.labels = append([]Label(nil), labels...)
	p.mu.Unlock()
}

func (p *IPCPresenter) SetPlacement(pl win.Placement) {
	p.mu.Lock()
	p.place = pl
	labels := p.labels
	frameID := p.frameID
	p.mu.Unlock()

	p.ensureStarted()

	if len(labels) == 0 || pl.ScreenW < 4 || pl.ScreenH < 4 {
		_ = p.proc.WriteOp(map[string]string{"op": "hide"})
		return
	}

	msgs := make([]map[string]any, 0, len(labels))
	for _, lab := range labels {
		if !lab.OutlineOnly && lab.Text == "" {
			continue
		}
		x0, y0, x1, y1 := boxAABB(lab.Box)
		msgs = append(msgs, map[string]any{
			"text":         lab.Text,
			"roman":        lab.Roman,
			"x":            x0,
			"y":            y0,
			"w":            x1 - x0,
			"h":            y1 - y0,
			"outline_only": lab.OutlineOnly,
		})
	}
	if len(msgs) == 0 {
		_ = p.proc.WriteOp(map[string]string{"op": "hide"})
		return
	}

	_ = p.proc.WriteOp(map[string]any{
		"op":       "ocr_labels",
		"req_id":   frameID,
		"screen_x": pl.ScreenX,
		"screen_y": pl.ScreenY,
		"screen_w": pl.ScreenW,
		"screen_h": pl.ScreenH,
		"labels":   msgs,
	})
}

func (p *IPCPresenter) Clear() {
	p.mu.Lock()
	p.labels = nil
	p.mu.Unlock()
	if p.proc.IsRunning() {
		_ = p.proc.WriteOp(map[string]string{"op": "hide"})
	}
}

func (p *IPCPresenter) Close() {
	p.proc.Stop()
}

func (p *IPCPresenter) VRStatus() (seen, ok bool, detail string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.vrSeen, p.vrOK, p.vrDetail
}

func (p *IPCPresenter) handleStdout(line string) {
	var head struct {
		Event string `json:"event"`
	}
	if json.Unmarshal([]byte(line), &head) == nil {
		switch head.Event {
		case "vr_status":
			var ev struct {
				OK     bool   `json:"ok"`
				Detail string `json:"detail"`
			}
			_ = json.Unmarshal([]byte(line), &ev)
			p.mu.Lock()
			p.vrSeen = true
			p.vrOK = ev.OK
			p.vrDetail = ev.Detail
			p.mu.Unlock()
			return
		case "caption_timing":
			var t struct {
				ReqID   uint64           `json:"req_id"`
				SpansUS map[string]int64 `json:"spans_us"`
			}
			if json.Unmarshal([]byte(line), &t) == nil && t.ReqID != 0 {
				latency.RecordOverlay(t.ReqID, map[string]int64{
					"render":  t.SpansUS["render"],
					"present": t.SpansUS["present"],
				})
			}
			return
		}
	}
	log.Printf("wt-overlay: %s", line)
}

func boxAABB(b ocr.Box) (x0, y0, x1, y1 int) {
	minX, maxX := b.X1, b.X1
	minY, maxY := b.Y1, b.Y1
	for _, x := range []float32{b.X2, b.X3, b.X4} {
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
	}
	for _, y := range []float32{b.Y2, b.Y3, b.Y4} {
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
	}
	return int(minX), int(minY), int(maxX), int(maxY)
}
