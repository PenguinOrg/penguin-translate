//go:build !windows

package engine

func killProcessTree(pid int) {}

func killProcessesOnPort(port string) {}

func killEnginePythonUnder(dataDir, engineDir string) {}
