package timing

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

type StartupTimer struct {
	t0   time.Time
	last time.Time
}

func NewStartupTimer() *StartupTimer {
	now := time.Now()
	return &StartupTimer{t0: now, last: now}
}

func (st *StartupTimer) Mark(step string) {
	if st == nil {
		return
	}
	now := time.Now()
	stepDur := now.Sub(st.last).Round(time.Millisecond)
	total := now.Sub(st.t0).Round(time.Millisecond)
	log.Printf("startup step=%s step_ms=%s total_ms=%s", step, stepDur, total)
	st.last = now
}

func (st *StartupTimer) Done() {
	if st == nil {
		return
	}
	total := time.Since(st.t0).Round(time.Millisecond)
	log.Printf("startup complete total_ms=%s", total)
}

func DurationMS(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000.0
}

func InjectGoTimings(body []byte, goTimings map[string]float64) []byte {
	if len(body) == 0 || len(goTimings) == 0 {
		return body
	}
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return body
	}
	existing, _ := m["timings_ms"].(map[string]any)
	if existing == nil {
		existing = map[string]any{}
	}
	for k, v := range goTimings {
		existing[k] = v
	}
	m["timings_ms"] = existing
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

func LogTimingsMS(label string, payload map[string]any) {
	raw, ok := payload["timings_ms"].(map[string]any)
	if !ok || len(raw) == 0 {
		return
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, raw[k]))
	}
	log.Printf("%s timings_ms: %s", label, strings.Join(parts, " "))
}

var engineMeta struct {
	mu        sync.RWMutex
	pid       int
	uptimeSec float64
	whisper   bool
	nllb      bool
	cutlet    bool
	updated   time.Time
}

func RecordManagedEngineStart(pid int) {
	engineMeta.mu.Lock()
	defer engineMeta.mu.Unlock()
	engineMeta.pid = pid
	engineMeta.updated = time.Now()
}

func UpdateEngineMetaFromHealth(body []byte) {
	var h struct {
		PID          int     `json:"pid"`
		UptimeSec    float64 `json:"uptime_sec"`
		ModelsLoaded *struct {
			Whisper bool `json:"whisper"`
			NLLB    bool `json:"nllb"`
			Cutlet  bool `json:"cutlet"`
		} `json:"models_loaded"`
	}
	if json.Unmarshal(body, &h) != nil {
		return
	}
	engineMeta.mu.Lock()
	defer engineMeta.mu.Unlock()
	if h.PID > 0 {
		engineMeta.pid = h.PID
	}
	if h.UptimeSec > 0 {
		engineMeta.uptimeSec = h.UptimeSec
	}
	if h.ModelsLoaded != nil {
		engineMeta.whisper = h.ModelsLoaded.Whisper
		engineMeta.nllb = h.ModelsLoaded.NLLB
		engineMeta.cutlet = h.ModelsLoaded.Cutlet
	}
	engineMeta.updated = time.Now()
}

func TakeManagedEnginePID() int {
	engineMeta.mu.Lock()
	defer engineMeta.mu.Unlock()
	pid := engineMeta.pid
	engineMeta.pid = 0
	return pid
}

func engineMetaSnapshot() (pid int, uptimeSec float64, whisper, nllb, cutlet bool) {
	engineMeta.mu.RLock()
	defer engineMeta.mu.RUnlock()
	return engineMeta.pid, engineMeta.uptimeSec, engineMeta.whisper, engineMeta.nllb, engineMeta.cutlet
}

func LogEngineMeta(prefix string) {
	pid, uptime, whisper, nllb, cutlet := engineMetaSnapshot()
	if pid <= 0 {
		return
	}
	log.Printf("%s engine_pid=%d engine_uptime_sec=%.0f models whisper=%v nllb=%v cutlet=%v",
		prefix, pid, uptime, whisper, nllb, cutlet)
}
