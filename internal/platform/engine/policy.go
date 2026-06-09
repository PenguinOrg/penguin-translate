package engine

import (
	"sync/atomic"

	"translation-overlay/internal/platform/domain"
)

var settingsLoader func() (domain.Settings, error)

var managedEngineSkipped atomic.Bool

func ManagedEngineSkipped() bool { return managedEngineSkipped.Load() }

func setManagedEngineSkipped(v bool) {
	managedEngineSkipped.Store(v)
}

func managedEngineRequired() bool {
	if settingsLoader == nil {
		return true
	}
	st, err := settingsLoader()
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
