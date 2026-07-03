package engine

import (
	"sync/atomic"

	"translation-overlay/internal/platform/domain"
)

var settingsLoader atomic.Pointer[func() (domain.Settings, error)]

func loadSettingsFn() func() (domain.Settings, error) {
	if p := settingsLoader.Load(); p != nil {
		return *p
	}
	return nil
}

var managedEngineSkipped atomic.Bool

func ManagedEngineSkipped() bool { return managedEngineSkipped.Load() }

func setManagedEngineSkipped(v bool) {
	managedEngineSkipped.Store(v)
}

func managedEngineRequired() bool {
	load := loadSettingsFn()
	if load == nil {
		return true
	}
	st, err := load()
	if err != nil {
		return true
	}
	return domain.RequiresManagedEngine(st)
}

func ManagedEngineAvailable() bool {
	if !useManagedEngine() {
		return true
	}
	if ManagedEngineSkipped() {
		return false
	}
	return true
}
