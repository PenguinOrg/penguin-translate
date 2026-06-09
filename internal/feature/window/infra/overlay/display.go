//go:build windows

package overlay

import (
	"sync"

	windows "golang.org/x/sys/windows"
	"translation-overlay/internal/feature/window/infra/win"
)

const presentPosEpsilon = 3

type FrameMarker interface {
	BeginFrame(id uint64)
}

type Display struct {
	mu         sync.Mutex
	show       bool
	inner      Presenter
	lastPlace  win.Placement
	lastLabels []Label
}

func NewDisplay(inner Presenter) *Display {
	if inner == nil {
		return nil
	}
	return &Display{inner: inner, show: false}
}

func (d *Display) SetVisible(on bool) {
	if d == nil {
		return
	}
	d.mu.Lock()
	was := d.show
	d.show = on
	inner := d.inner
	d.mu.Unlock()
	if !on && inner != nil {
		d.lastLabels = nil
		inner.Clear()
	}
	if on && !was && inner != nil {
	}
}

func (d *Display) Visible() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.show
}

func (d *Display) Present(target windows.Handle, place win.Placement, labels []Label) {
	d.PresentFrame(target, place, labels, 0)
}

func (d *Display) PresentFrame(target windows.Handle, place win.Placement, labels []Label, frameID uint64) {
	if d == nil || !d.Visible() {
		return
	}
	d.mu.Lock()
	inner := d.inner
	if inner == nil {
		d.mu.Unlock()
		return
	}
	if len(labels) == 0 {
		d.lastLabels = nil
		d.mu.Unlock()
		inner.Attach(target)
		inner.Clear()
		return
	}
	if labelsEqual(d.lastLabels, labels) && d.lastPlace.NearEqual(place, presentPosEpsilon) {
		d.mu.Unlock()
		return
	}
	d.lastLabels = append([]Label(nil), labels...)
	d.lastPlace = place
	d.mu.Unlock()

	if fm, ok := inner.(FrameMarker); ok {
		fm.BeginFrame(frameID)
	}
	inner.Attach(target)
	inner.SetLabels(labels)
	inner.SetPlacement(place)
}
