package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"translation-overlay/internal/platform/timing"
)

func prepareEngine(ctx context.Context) error {
	sink := launcherStatus
	st := timing.NewStartupTimer()
	defer st.Done()

	if !useManagedEngine() {
		log.Printf("external engine %s (set TO_ENGINE or clear TO_SKIP_MANAGED_ENGINE)", engineURL())
		setManagedEngineSkipped(false)
		sink.setPhase(PhaseReady, "External engine", 100)
		sink.ready()
		st.Mark("external_engine")
		return nil
	}

	if !managedEngineRequired() {
		log.Print("managed engine skipped — cloud-native mode (no local Whisper/NLLB/Python sidecar)")
		setManagedEngineSkipped(true)
		sink.setPhase(PhaseReady, "Cloud-native — Python engine not required", 100)
		sink.ready()
		st.Mark("cloud_native")
		return nil
	}
	setManagedEngineSkipped(false)

	dataDir, err := appDataDir()
	if err != nil {
		sink.fail(err)
		return err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		sink.fail(err)
		return err
	}
	sink.setDataDir(dataDir)

	sink.setPhase(PhaseSyncEngine, "Unpacking engine", -1)
	engineDir := filepath.Join(dataDir, "engine")
	killStaleEnginePython(engineDir)
	time.Sleep(400 * time.Millisecond)
	if err := syncEngineBundle(engineDir); err != nil {
		sink.fail(err)
		return err
	}
	st.Mark("sync_engine")

	base := managedEngineBaseURL()
	_ = os.Setenv("TO_ENGINE", base)
	log.Printf("managed inference engine → %s", base)

	if err := startManagedEngine(ctx, dataDir, engineDir, sink); err != nil {
		sink.fail(err)
		return err
	}
	st.Mark("venv_and_start_engine")
	if err := waitEngineHealthy(ctx, base, 3*time.Minute, sink); err != nil {
		sink.fail(err)
		return err
	}
	st.Mark("wait_health")
	sink.ready()
	return nil
}

type LoadOptions struct {
	Whisper bool `json:"whisper"`
	NLLB    bool `json:"nllb"`
	Cutlet  bool `json:"cutlet"`
}

func TriggerEngineLoadWithOptions(ctx context.Context, base string, opts LoadOptions) error {
	body, err := json.Marshal(opts)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/load", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	cl := &http.Client{Timeout: 20 * time.Minute}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode != http.StatusOK {
		return &loadError{status: resp.StatusCode, body: string(respBody)}
	}
	timing.UpdateEngineMetaFromHealth(respBody)
	var snap engineLoadSnapshot
	if json.Unmarshal(respBody, &snap) == nil && snap.Status != "" {
		lastLoadSnapshot = snap
	}
	var loadOut map[string]any
	if json.Unmarshal(respBody, &loadOut) == nil {
		timing.LogTimingsMS("engine_load", loadOut)
	}
	return nil
}

type loadError struct {
	status int
	body   string
}

func (e *loadError) Error() string {
	return "load HTTP " + http.StatusText(e.status) + ": " + e.body
}

type engineLoadSnapshot struct {
	Status string `json:"status"`
	Device string `json:"device"`
	Detail string `json:"device_detail"`
}

var lastLoadSnapshot engineLoadSnapshot

func TryFetchLoadSnapshot(ctx context.Context) { tryFetchLoadSnapshot(ctx) }

func LastLoadSnapshot() engineLoadSnapshot { return lastLoadSnapshot }

func tryFetchLoadSnapshot(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, engineURL()+"/health", nil)
	if err != nil {
		return
	}
	cl := &http.Client{Timeout: 2 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	timing.UpdateEngineMetaFromHealth(body)
	var out engineLoadSnapshot
	_ = json.NewDecoder(bytes.NewReader(body)).Decode(&out)
	if out.Status != "" {
		lastLoadSnapshot = out
	}
}
