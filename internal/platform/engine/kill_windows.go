//go:build windows

package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"translation-overlay/internal/platform/osproc"
)

func killProcessTree(pid int) {
	if pid <= 0 {
		return
	}
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	cmd.Stdout = nil
	cmd.Stderr = nil
	osproc.Hide(cmd)
	_ = cmd.Run()
}

func killProcessesOnPort(port string) {
	port = strings.TrimSpace(port)
	if port == "" {
		return
	}
	script := `
$port = ` + port + `
$conns = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
foreach ($c in $conns) {
  $id = [int]$c.OwningProcess
  if ($id -gt 0) {
    Stop-Process -Id $id -Force -ErrorAction SilentlyContinue
    & taskkill.exe /F /T /PID $id 2>$null | Out-Null
  }
}
`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	osproc.Hide(cmd)
	_ = cmd.Run()
}

func killEnginePythonUnder(dataDir, engineDir string) {
	dataAbs, _ := filepath.Abs(dataDir)
	engineAbs, _ := filepath.Abs(engineDir)
	if dataAbs == "" {
		dataAbs = dataDir
	}
	if engineAbs == "" {
		engineAbs = engineDir
	}
	script := `
$data = $env:TO_DATA_DIR_KILL
$engine = $env:TO_ENGINE_DIR_KILL
Get-CimInstance Win32_Process -Filter "Name='python.exe'" -ErrorAction SilentlyContinue | ForEach-Object {
  $cl = $_.CommandLine
  if (-not $cl) { return }
  $match = ($cl -like "*server.py*" -and (
    $cl -like ("*" + $data + "*") -or
    $cl -like ("*" + $engine + "*") -or
    $cl -like "*translation-overlay*"
  ))
  if ($match) {
    & taskkill.exe /F /T /PID $_.ProcessId 2>$null | Out-Null
  }
}
`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(),
		"TO_DATA_DIR_KILL="+dataAbs,
		"TO_ENGINE_DIR_KILL="+engineAbs,
	)
	osproc.Hide(cmd)
	_ = cmd.Run()
}
