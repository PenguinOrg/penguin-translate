//go:build windows

package overlay

import (
	"fmt"

	windows "golang.org/x/sys/windows"
	"translation-overlay/internal/feature/window/infra/ocr"
	"translation-overlay/internal/feature/window/infra/win"
)

type Presenter interface {
	Attach(target windows.Handle)
	SetPlacement(p win.Placement)
	SetLabels(labels []Label)
	Clear()
	Close()
}

type Label struct {
	Text        string
	Roman       string
	Box         ocr.Box
	OutlineOnly bool
}

func labelsEqual(a, b []Label) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].OutlineOnly != b[i].OutlineOnly || a[i].Text != b[i].Text || a[i].Roman != b[i].Roman {
			return false
		}
		if boxKey(a[i].Box) != boxKey(b[i].Box) {
			return false
		}
	}
	return true
}

func boxKey(b ocr.Box) string {
	q := func(v float32) int { return int((float64(v) + 4) / 8) }
	return fmt.Sprintf("%d,%d,%d,%d,%d,%d,%d,%d",
		q(b.X1), q(b.Y1), q(b.X2), q(b.Y2),
		q(b.X3), q(b.Y3), q(b.X4), q(b.Y4),
	)
}
