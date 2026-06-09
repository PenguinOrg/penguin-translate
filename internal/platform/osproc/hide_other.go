//go:build !windows

package osproc

import "os/exec"

func Hide(cmd *exec.Cmd) {}
