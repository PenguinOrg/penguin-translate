//go:build windows

package overlay

import "testing"

func TestIPCPresenterHandlesTogglePause(t *testing.T) {
	called := 0
	p := NewIPCPresenter(false, true, 1.2, 1.6, func() { called++ })
	p.handleStdout(`{"event":"toggle_pause"}`)
	if called != 1 {
		t.Fatalf("toggle callback called %d times, want 1", called)
	}
}
