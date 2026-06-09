package engine

import (
	"log"
	"net"
	"path/filepath"
	"strings"
	"time"

	"translation-overlay/internal/platform/timing"
)

func stopManagedEngine() {
	if !useManagedEngine() {
		return
	}
	if ManagedEngineSkipped() {
		return
	}
	base := managedEngineBaseURL()
	port := portFromBaseURL(base)

	requestEngineShutdown(base)

	if cmd := takeManagedCmd(); cmd != nil && cmd.Process != nil {
		killProcessTree(cmd.Process.Pid)
	}

	dataDir, err := appDataDir()
	if err == nil {
		killEnginePythonUnder(dataDir, filepath.Join(dataDir, "engine"))
	} else {
		log.Printf("engine stop: data dir: %v", err)
	}
	killProcessesOnPort(port)

	if pid := timing.TakeManagedEnginePID(); pid > 0 {
		killProcessTree(pid)
	}

	if waitUntilPortFree(port, 8*time.Second) {
		log.Printf("engine stopped (port %s free)", port)
	} else {
		log.Printf("engine stop: port %s still in use after timeout — kill python manually if VRAM is stuck", port)
		killProcessesOnPort(port)
	}
}

func waitUntilPortFree(port string, maxWait time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if !enginePortOpen(port) {
			return true
		}
		killProcessesOnPort(port)
		if dataDir, err := appDataDir(); err == nil {
			killEnginePythonUnder(dataDir, filepath.Join(dataDir, "engine"))
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
