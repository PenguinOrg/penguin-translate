//go:build windows

package osproc

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func Hide(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	sa := cmd.SysProcAttr
	if sa == nil {
		sa = &syscall.SysProcAttr{}
		cmd.SysProcAttr = sa
	}
	sa.HideWindow = true
	sa.CreationFlags |= createNoWindow
}
