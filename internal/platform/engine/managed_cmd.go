package engine

import (
	"os/exec"
	"sync"
)

var (
	managedCmdMu sync.Mutex
	managedCmd   *exec.Cmd
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
