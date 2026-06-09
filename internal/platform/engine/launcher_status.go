package engine

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type LauncherStatus struct {
	Phase     string   `json:"phase"`
	Label     string   `json:"label"`
	Percent   int      `json:"percent"`
	LogTail   []string `json:"log_tail"`
	Error     string   `json:"error"`
	DataDir   string   `json:"data_dir"`
	UpdatedAt int64    `json:"updated_at"`
}

const (
	PhaseInit          = "init"
	PhaseDetectPython  = "detect_python"
	PhaseSyncEngine    = "sync_engine"
	PhaseCreateVenv    = "create_venv"
	PhasePipUpgrade    = "pip_upgrade"
	PhasePipInstall    = "pip_install"
	PhaseStartEngine   = "start_engine"
	PhaseWaitHealth    = "wait_health"
	PhaseModelsLoading = "models_loading"
	PhaseReady         = "ready"
	PhaseFailed        = "failed"
)

func HandleLauncherStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(launcherStatus.Snapshot())
}

type statusSink struct {
	mu      sync.Mutex
	current LauncherStatus
	logRing []string
}

const logTailMax = 30

func Launcher() *statusSink { return launcherStatus }

var launcherStatus = &statusSink{
	current: LauncherStatus{
		Phase:     PhaseInit,
		Label:     "Starting…",
		Percent:   -1,
		UpdatedAt: time.Now().Unix(),
	},
}

func (s *statusSink) Snapshot() LauncherStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.current
	if out.LogTail == nil {
		out.LogTail = []string{}
	} else {
		cp := make([]string, len(out.LogTail))
		copy(cp, out.LogTail)
		out.LogTail = cp
	}
	return out
}

func (s *statusSink) setPhase(phase, label string, percent int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current.Phase = phase
	s.current.Label = label
	s.current.Percent = percent
	s.current.UpdatedAt = time.Now().Unix()
	if phase != PhaseFailed {
		s.current.Error = ""
	}
}

func (s *statusSink) setLabel(label string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current.Label = label
	s.current.UpdatedAt = time.Now().Unix()
}

func (s *statusSink) setDataDir(d string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current.DataDir = d
}

func (s *statusSink) appendLog(line string) {
	if line == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logRing = append(s.logRing, line)
	if len(s.logRing) > logTailMax {
		s.logRing = s.logRing[len(s.logRing)-logTailMax:]
	}
	cp := make([]string, len(s.logRing))
	copy(cp, s.logRing)
	s.current.LogTail = cp
	s.current.UpdatedAt = time.Now().Unix()
}

func (s *statusSink) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current.Phase = PhaseFailed
	if err != nil {
		s.current.Error = err.Error()
	} else if s.current.Error == "" {
		s.current.Error = "unknown error"
	}
	s.current.UpdatedAt = time.Now().Unix()
}

func (s *statusSink) ready() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current.Phase = PhaseReady
	s.current.Label = "Ready"
	s.current.Percent = 100
	s.current.Error = ""
	s.current.UpdatedAt = time.Now().Unix()
}
