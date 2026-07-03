package port

import "translation-overlay/internal/platform/domain"

type SettingsRepository interface {
	Load() (domain.Settings, error)
	Save(domain.Settings) error
	// Update runs mutate on the current settings and persists the result under
	// the repository's write lock, so concurrent writers cannot lose updates.
	Update(mutate func(*domain.Settings) error) (domain.Settings, error)
	Path() string
	Exists() bool
}
