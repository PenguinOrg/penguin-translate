package persist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"translation-overlay/internal/platform/domain"
	"translation-overlay/internal/platform/port"
)

type JSONRepository struct {
	mu     sync.RWMutex
	path   string
	cached domain.Settings
	loaded bool
}

func NewJSONRepository(path string) *JSONRepository {
	return &JSONRepository{path: path}
}

func (r *JSONRepository) Path() string { return r.path }

func (r *JSONRepository) Exists() bool {
	_, err := os.Stat(r.path)
	return err == nil
}

func (r *JSONRepository) Load() (domain.Settings, error) {
	r.mu.RLock()
	if r.loaded {
		st := cloneSettings(r.cached)
		r.mu.RUnlock()
		return st, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loaded {
		return cloneSettings(r.cached), nil
	}
	st, err := r.loadUnlocked()
	if err != nil {
		return domain.Settings{}, err
	}
	r.cached = cloneSettings(st)
	r.loaded = true
	return st, nil
}

func (r *JSONRepository) loadUnlocked() (domain.Settings, error) {
	b, err := os.ReadFile(r.path)
	if err != nil {
		return domain.Settings{}, err
	}
	var st domain.Settings
	if err := json.Unmarshal(b, &st); err != nil {
		return domain.Settings{}, err
	}
	normalize(&st)
	return st, nil
}

func (r *JSONRepository) Save(st domain.Settings) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.saveUnlocked(st); err != nil {
		return err
	}
	normalize(&st)
	r.cached = cloneSettings(st)
	r.loaded = true
	return nil
}

func (r *JSONRepository) Update(mutate func(*domain.Settings) error) (domain.Settings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var st domain.Settings
	if r.loaded {
		st = cloneSettings(r.cached)
	} else {
		var err error
		st, err = r.loadUnlocked()
		if err != nil {
			return domain.Settings{}, err
		}
	}
	if err := mutate(&st); err != nil {
		return domain.Settings{}, err
	}
	if err := r.saveUnlocked(st); err != nil {
		return domain.Settings{}, err
	}
	normalize(&st)
	r.cached = cloneSettings(st)
	r.loaded = true
	return cloneSettings(st), nil
}

func cloneSettings(st domain.Settings) domain.Settings {
	if st.MicTranslate.Plugins != nil {
		plugins := make(map[string]json.RawMessage, len(st.MicTranslate.Plugins))
		for k, v := range st.MicTranslate.Plugins {
			cp := make(json.RawMessage, len(v))
			copy(cp, v)
			plugins[k] = cp
		}
		st.MicTranslate.Plugins = plugins
	}
	if st.Window.SkipWords != nil {
		skip := make([]string, len(st.Window.SkipWords))
		copy(skip, st.Window.SkipWords)
		st.Window.SkipWords = skip
	}
	return st
}

// saveUnlocked writes to a temp file then renames it over the target so a
// crash mid-write can never leave a truncated settings.json.
func (r *JSONRepository) saveUnlocked(st domain.Settings) error {
	normalize(&st)
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".settings-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), r.path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

var _ port.SettingsRepository = (*JSONRepository)(nil)
