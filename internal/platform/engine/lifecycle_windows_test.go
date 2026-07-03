//go:build windows

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	rootembed "translation-overlay"
	"translation-overlay/internal/platform/domain"
)

const lifecyclePort = "18745"

// Boots the real Python engine (venv reused via junction, pip skipped) and
// asserts Shutdown kills the process and frees the port.
func TestManagedEngineLifecycle(t *testing.T) {
	if os.Getenv("TO_ENGINE_LIFECYCLE_TEST") == "" {
		t.Skip("set TO_ENGINE_LIFECYCLE_TEST=1 — boots the real Python engine (needs a populated venv; override with TO_LIFECYCLE_VENV)")
	}
	venv := strings.TrimSpace(os.Getenv("TO_LIFECYCLE_VENV"))
	if venv == "" {
		venv = filepath.Join(os.Getenv("LOCALAPPDATA"), "translation-overlay", "venv")
	}
	if _, err := os.Stat(filepath.Join(venv, "Scripts", "python.exe")); err != nil {
		t.Skipf("no venv python under %s: %v", venv, err)
	}

	dataDir := t.TempDir()
	t.Setenv("TO_DATA_DIR", dataDir)
	t.Setenv("TO_ENGINE_PORT", lifecyclePort)
	if url := EngineURL(); !strings.HasSuffix(url, ":"+lifecyclePort) {
		t.Fatalf("engine env snapshot predates test setup: %s", url)
	}

	link := filepath.Join(dataDir, "venv")
	junction := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"New-Item -ItemType Junction -Path $env:J_LINK -Target $env:J_TARGET | Out-Null")
	junction.Env = append(os.Environ(), "J_LINK="+link, "J_TARGET="+venv)
	if out, err := junction.CombinedOutput(); err != nil {
		t.Fatalf("venv junction: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) })

	req, err := fs.ReadFile(rootembed.EmbeddedInference, "runtime/inference/requirements.txt")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(req)
	if err := os.WriteFile(filepath.Join(dataDir, ".requirements_sha256"), []byte(hex.EncodeToString(sum[:])), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	loader := func() (domain.Settings, error) { return domain.Settings{}, errors.New("force managed engine") }
	if err := Prepare(ctx, loader); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !enginePortOpen(lifecyclePort) {
		t.Fatal("engine reported healthy but port is not open")
	}
	pid := portOwnerPID(t, lifecyclePort)
	if pid <= 0 {
		t.Fatal("no owning pid for engine port")
	}
	t.Logf("engine up on :%s, pid %d", lifecyclePort, pid)

	Shutdown()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && (processAlive(pid) || enginePortOpen(lifecyclePort)) {
		time.Sleep(300 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatalf("engine pid %d still alive after Shutdown", pid)
	}
	if enginePortOpen(lifecyclePort) {
		t.Fatal("engine port still open after Shutdown")
	}
	Shutdown()
}

func portOwnerPID(t *testing.T, port string) int {
	t.Helper()
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"(Get-NetTCPConnection -LocalPort ([int]$env:Q_PORT) -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1).OwningProcess")
	cmd.Env = append(os.Environ(), "Q_PORT="+port)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("port owner query: %v", err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return pid
}

func processAlive(pid int) bool {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"if (Get-Process -Id ([int]$env:Q_PID) -ErrorAction SilentlyContinue) { 'alive' }")
	cmd.Env = append(os.Environ(), "Q_PID="+strconv.Itoa(pid))
	out, _ := cmd.Output()
	return strings.Contains(string(out), "alive")
}
