package host

import (
	"sync"

	"translation-overlay/internal/platform/port"
)

type Host struct {
	repo port.SettingsRepository
	once sync.Once
}

func New(repo port.SettingsRepository) *Host {
	return &Host{repo: repo}
}
