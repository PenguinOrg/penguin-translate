package host

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"translation-overlay/internal/platform/engine"
)

type debugLogsJSON struct {
	LauncherLog string   `json:"launcher_log"`
	EngineLog   string   `json:"engine_log"`
	LogPaths    []string `json:"log_paths"`
	EngineURL   string   `json:"engine_url"`
	EngineTitle string   `json:"engine_title,omitempty"`
	EngineOK    bool     `json:"engine_identity_ok"`
}

func tailFile(path string, maxLines int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}

func (h *Host) handleDebugLogs(w http.ResponseWriter, r *http.Request) {
	if !isGetOrHead(r) {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodHead {
		return
	}
	maxLines := 80
	if q := strings.TrimSpace(r.URL.Query().Get("lines")); q != "" {
		if n, err := strconv.Atoi(q); err == nil {
			maxLines = n
		}
	}
	if maxLines < 20 {
		maxLines = 20
	}
	if maxLines > 500 {
		maxLines = 500
	}
	dataDir, _ := h.appDataDir()
	launcherPath := ""
	enginePath := ""
	if dataDir != "" {
		launcherPath = filepath.Join(dataDir, "launcher.log")
		enginePath = filepath.Join(dataDir, "engine.log")
	}
	base := engineURL()
	ok, title, _ := engine.ProbeEngineIdentity(r.Context(), base, httpClientShort)
	out := debugLogsJSON{
		LauncherLog: tailFile(launcherPath, maxLines),
		EngineLog:   tailFile(enginePath, maxLines),
		LogPaths:    []string{launcherPath, enginePath},
		EngineURL:   base,
		EngineTitle: title,
		EngineOK:    ok,
	}
	_ = json.NewEncoder(w).Encode(out)
}
