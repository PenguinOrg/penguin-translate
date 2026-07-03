package engine

import (
	"os/exec"
	"sync"
	"sync/atomic"
)

var (
	managedCmdMu sync.Mutex
	managedCmd   *exec.Cmd

	// True only if this process launched the engine subprocess; Shutdown keys
	// off this, not env-derived config.
	managedEngineStarted atomic.Bool
)

func setManagedCmd(cmd *exec.Cmd) {
	managedCmdMu.Lock()
	managedCmd = cmd
	managedCmdMu.Unlock()
}

func clearManagedCmd(cmd *exec.Cmd) {
	managedCmdMu.Lock()
	if managedCmd == cmd {
		managedCmd = nil
	}
	managedCmdMu.Unlock()
}

func takeManagedCmd() *exec.Cmd {
	managedCmdMu.Lock()
	cmd := managedCmd
	managedCmd = nil
	managedCmdMu.Unlock()
	return cmd
}
