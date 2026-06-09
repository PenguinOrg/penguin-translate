//go:build windows

package main

import (
	"log"

	win "translation-overlay/internal/feature/window/infra/win"
)

func init() {
	if win.EnableDpiAwareness() {
		log.Print("per-monitor DPI awareness enabled (window-translate overlay alignment)")
	}
}
