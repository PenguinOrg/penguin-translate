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

// Kills the port's listener only when verified as our engine: image under the
// app venv, or python running server.py from the app data/engine dirs.
func killEngineProcessOnPort(port, dataDir, engineDir string) {
	port = strings.TrimSpace(port)
	if port == "" {
		return
	}
	dataAbs, _ := filepath.Abs(dataDir)
	engineAbs, _ := filepath.Abs(engineDir)
	if dataAbs == "" {
		dataAbs = dataDir
	}
	if engineAbs == "" {
		engineAbs = engineDir
	}
	venvAbs := filepath.Join(dataAbs, "venv")
	// The dirs are used as -like patterns; unescaped wildcard chars ([ ]) in a
	// path make verification fail silently and the stale engine survives.
	script := `
$port = [int]$env:TO_PORT_KILL
$venv = [WildcardPattern]::Escape($env:TO_VENV_DIR_KILL)
$data = [WildcardPattern]::Escape($env:TO_DATA_DIR_KILL)
$engine = [WildcardPattern]::Escape($env:TO_ENGINE_DIR_KILL)
Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue | ForEach-Object {
  $id = [int]$_.OwningProcess
  if ($id -le 0) { return }
  $p = Get-CimInstance Win32_Process -Filter "ProcessId=$id" -ErrorAction SilentlyContinue
  if (-not $p) { return }
  $ours = $false
  if ($p.ExecutablePath -and ($p.ExecutablePath -like ($venv + "\*"))) { $ours = $true }
  $cl = $p.CommandLine
  if (-not $ours -and $p.Name -like "python*" -and $cl -and $cl -like "*server.py*" -and (
      $cl -like ("*" + $data + "*") -or
      $cl -like ("*" + $engine + "*") -or
      $cl -like "*translation-overlay*")) { $ours = $true }
  if ($ours) {
    & taskkill.exe /F /T /PID $id 2>$null | Out-Null
  }
}
`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(),
		"TO_PORT_KILL="+port,
		"TO_VENV_DIR_KILL="+venvAbs,
		"TO_DATA_DIR_KILL="+dataAbs,
		"TO_ENGINE_DIR_KILL="+engineAbs,
	)
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
$data = [WildcardPattern]::Escape($env:TO_DATA_DIR_KILL)
$engine = [WildcardPattern]::Escape($env:TO_ENGINE_DIR_KILL)
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
