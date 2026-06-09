package port

import "translation-overlay/internal/platform/domain"

type SettingsRepository interface {
	Load() (domain.Settings, error)
	Save(domain.Settings) error
	Path() string
	Exists() bool
}
