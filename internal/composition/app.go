package composition

import (
	"path/filepath"

	audiohost "translation-overlay/internal/feature/audio/host"
	mictranslatehost "translation-overlay/internal/feature/mictranslate/host"
	windowhost "translation-overlay/internal/feature/window/host"
	"translation-overlay/internal/platform/persist"
	"translation-overlay/internal/platform/port"
)

type App struct {
	SettingsRepo port.SettingsRepository
	DataDir      string
	MicTranslate *mictranslatehost.Host
	Audio        *audiohost.Host
	Window       *windowhost.Host
}

func New(dataDir string) (*App, error) {
	settingsPath := filepath.Join(dataDir, "settings.json")
	repo := persist.NewJSONRepository(settingsPath)
	if !repo.Exists() {
		if err := repo.Save(persist.Default()); err != nil {
			return nil, err
		}
	}

	return &App{
		SettingsRepo: repo,
		DataDir:      dataDir,
		MicTranslate: mictranslatehost.New(repo),
		Audio:        audiohost.New(repo),
		Window:       windowhost.New(repo),
	}, nil
}
