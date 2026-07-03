package engine

import (
	"log"
	"net"
	"path/filepath"
	"strings"
	"time"
)

func stopManagedEngine() {
	if !managedEngineStarted.CompareAndSwap(true, false) {
		return
	}
	port := portFromBaseURL(managedEngineBaseURL())
	terminateEngineProcesses()
	if waitUntilPortFree(port, 8*time.Second) {
		log.Printf("engine stopped (port %s free)", port)
	} else {
		log.Printf("engine stop: port %s still in use after timeout — kill python manually if VRAM is stuck", port)
	}
}

func terminateEngineProcesses() {
	base := managedEngineBaseURL()
	port := portFromBaseURL(base)

	requestEngineShutdown(base)

	if cmd := takeManagedCmd(); cmd != nil && cmd.Process != nil {
		killProcessTree(cmd.Process.Pid)
	}

	dataDir, err := appDataDir()
	if err == nil {
		engineDir := filepath.Join(dataDir, "engine")
		killEnginePythonUnder(dataDir, engineDir)
		killEngineProcessOnPort(port, dataDir, engineDir)
	} else {
		log.Printf("engine stop: data dir: %v", err)
	}
}

func waitUntilPortFree(port string, maxWait time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if !enginePortOpen(port) {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return !enginePortOpen(port)
}

func enginePortOpen(port string) bool {
	port = strings.TrimSpace(port)
	if port == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 400*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
