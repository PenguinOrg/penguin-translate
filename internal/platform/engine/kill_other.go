//go:build !windows

package engine

func killProcessTree(pid int) {}

func killEngineProcessOnPort(port, dataDir, engineDir string) {}

func killEnginePythonUnder(dataDir, engineDir string) {}
