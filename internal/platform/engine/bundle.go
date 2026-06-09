package engine

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	rootembed "translation-overlay"
	"translation-overlay/internal/platform/applog"
	"translation-overlay/internal/platform/domain"
	"translation-overlay/internal/platform/osproc"
	"translation-overlay/internal/platform/timing"
)

const (
	defaultEnginePort = "8745"
)

func embeddedEngineFingerprint() (string, error) {
	h := sha256.New()
	err := fs.WalkDir(rootembed.EmbeddedInference, "runtime/inference", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(rootembed.EmbeddedInference, path)
		if err != nil {
			return err
		}
		_, _ = h.Write([]byte(path))
		_, _ = h.Write(b)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func killStaleEnginePython(_ string) {
	stopManagedEngine()
}

func appDataDir() (string, error) {
	if d := strings.TrimSpace(os.Getenv("TO_DATA_DIR")); d != "" {
		return d, nil
	}
	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return "", errors.New("LOCALAPPDATA is not set")
		}
		return filepath.Join(local, "translation-overlay"), nil
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "translation-overlay"), nil
	}
}

func extractEmbeddedEngine(engineDir string) error {
	return fs.WalkDir(rootembed.EmbeddedInference, "runtime/inference", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "runtime/inference" {
			return nil
		}
		rel := strings.TrimPrefix(path, "runtime/inference/")
		target := filepath.Join(engineDir, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		b, err := fs.ReadFile(rootembed.EmbeddedInference, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

func syncEngineBundle(engineDir string) error {
	start := time.Now()
	fp, err := embeddedEngineFingerprint()
	if err != nil {
		return err
	}
	verPath := filepath.Join(engineDir, ".bundle_ver")
	want := fp + "\n"
	cur, _ := os.ReadFile(verPath)
	if string(cur) == want {
		log.Printf("startup sync_engine: bundle unchanged (%s)", time.Since(start).Round(time.Millisecond))
		return nil
	}
	log.Printf("updating bundled engine in %s (bundle %s…)", engineDir, fp[:min(12, len(fp))])
	if err := os.RemoveAll(engineDir); err != nil && !os.IsNotExist(err) {
		log.Printf("engine dir remove failed (still in use?); overwriting in place: %v", err)
	}
	if err := os.MkdirAll(engineDir, 0o755); err != nil {
		return err
	}
	if err := extractEmbeddedEngine(engineDir); err != nil {
		return err
	}
	if err := os.WriteFile(verPath, []byte(want), 0o644); err != nil {
		return err
	}
	log.Printf("startup sync_engine: extracted bundle (%s)", time.Since(start).Round(time.Millisecond))
	return nil
}

func venvPython(dataDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(dataDir, "venv", "Scripts", "python.exe")
	}
	return filepath.Join(dataDir, "venv", "bin", "python")
}

func requirementsHash(engineDir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(engineDir, "requirements.txt"))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func findBasePython(ctx context.Context, sink *statusSink) (string, error) {
	if p := strings.TrimSpace(os.Getenv("TO_PYTHON")); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("TO_PYTHON not found: %s", p)
	}
	try := []struct {
		name string
		args []string
	}{
		{"py", []string{"-3"}},
		{"python3", nil},
		{"python", nil},
	}
	for _, t := range try {
		exe, err := exec.LookPath(t.name)
		if err != nil {
			continue
		}
		args := append(append([]string{}, t.args...), "-c", "import sys; print(sys.executable)")
		cmd := exec.CommandContext(ctx, exe, args...)
		osproc.Hide(cmd)
		out, err := cmd.CombinedOutput()
		if err != nil {
			continue
		}
		line := strings.TrimSpace(string(out))
		if line != "" {
			if sink != nil {
				sink.appendLog("python: " + line)
			}
			return line, nil
		}
	}
	return "", errors.New("no Python found on PATH (install Python 3.10+ or set TO_PYTHON)")
}

func streamCmd(cmd *exec.Cmd, sink *statusSink, fileSink io.Writer, onLine func(string)) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	osproc.Hide(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	pump := func(rc io.Reader) {
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), "\r\n")
			if line == "" {
				continue
			}
			if fileSink != nil {
				_, _ = io.WriteString(fileSink, line+"\n")
			}
			if sink != nil {
				sink.appendLog(line)
			}
			if onLine != nil {
				onLine(line)
			}
		}
	}
	done := make(chan struct{}, 2)
	go func() { pump(stdout); done <- struct{}{} }()
	go func() { pump(stderr); done <- struct{}{} }()
	<-done
	<-done
	return cmd.Wait()
}

func ensureVenvAndDeps(ctx context.Context, dataDir, engineDir string, sink *statusSink) (string, error) {
	depStart := time.Now()
	vpy := venvPython(dataDir)
	reqHash, err := requirementsHash(engineDir)
	if err != nil {
		return "", err
	}
	hashFile := filepath.Join(dataDir, ".requirements_sha256")

	if _, err := os.Stat(vpy); err != nil {
		sink.setPhase(PhaseDetectPython, "Locating Python", -1)
		base, err := findBasePython(ctx, sink)
		if err != nil {
			return "", err
		}
		sink.setPhase(PhaseCreateVenv, "Creating virtual environment", -1)
		log.Printf("creating venv with %s", base)
		venvDir := filepath.Join(dataDir, "venv")
		cmd := exec.CommandContext(ctx, base, "-m", "venv", venvDir)
		if err := streamCmd(cmd, sink, nil, nil); err != nil {
			return "", fmt.Errorf("venv: %w", err)
		}
	}

	prev, _ := os.ReadFile(hashFile)
	if strings.TrimSpace(string(prev)) == reqHash {
		log.Printf("startup venv_deps: cached (%s)", time.Since(depStart).Round(time.Millisecond))
		return vpy, nil
	}

	sink.setPhase(PhasePipUpgrade, "Upgrading pip", -1)
	log.Printf("installing Python dependencies (first run can take several minutes)…")
	upCmd := exec.CommandContext(ctx, vpy, "-m", "pip", "install", "--upgrade", "pip")
	if err := streamCmd(upCmd, sink, nil, nil); err != nil {
		return "", fmt.Errorf("pip upgrade: %w", err)
	}

	sink.setPhase(PhasePipInstall, "Installing Python packages", -1)
	req := filepath.Join(engineDir, "requirements.txt")
	args := []string{"-m", "pip", "install", "-r", req}
	args = append(args, strings.Fields(os.Getenv("TO_PIP_EXTRA"))...)
	piCmd := exec.CommandContext(ctx, vpy, args...)

	verbs := []string{"Collecting ", "Downloading ", "Installing ", "Building ", "Preparing ", "Requirement already satisfied: "}
	if err := streamCmd(piCmd, sink, nil, func(line string) {
		for _, v := range verbs {
			if strings.HasPrefix(line, v) {
				short := line
				if len(short) > 80 {
					short = short[:80] + "…"
				}
				sink.setLabel(short)
				break
			}
		}
	}); err != nil {
		return "", fmt.Errorf("pip install -r requirements.txt: %w", err)
	}

	if err := os.WriteFile(hashFile, []byte(reqHash), 0o644); err != nil {
		return "", err
	}
	log.Printf("startup venv_deps: pip install complete (%s)", time.Since(depStart).Round(time.Millisecond))
	return vpy, nil
}

func startManagedEngine(ctx context.Context, dataDir, engineDir string, sink *statusSink) error {
	vpy, err := ensureVenvAndDeps(ctx, dataDir, engineDir, sink)
	if err != nil {
		return err
	}
	sink.setPhase(PhaseStartEngine, "Starting inference engine", -1)
	logPath := filepath.Join(dataDir, "engine.log")
	lf, err := applog.NewRotatingWriter(logPath, 5<<20, 3)
	if err != nil {
		return err
	}
	_, _ = lf.Write([]byte("\n---- " + time.Now().Format(time.RFC3339) + " ----\n"))

	serverPy := filepath.Join(engineDir, "server.py")
	cmd := exec.CommandContext(ctx, vpy, serverPy)
	cmd.Dir = engineDir
	base := managedEngineBaseURL()
	port := strings.TrimPrefix(strings.TrimPrefix(base, "http://"), "https://")
	if i := strings.LastIndex(port, ":"); i >= 0 {
		port = port[i+1:]
	}
	weightsDir := filepath.Join(dataDir, "weights")
	_ = os.MkdirAll(weightsDir, 0o755)
	cmd.Env = append(os.Environ(), "TO_ENGINE_PORT="+port)
	wGPU, nGPU := whisperGPU(), nllbGPU()
	cmd.Env = append(cmd.Env,
		"CUDA_DEVICE_ORDER=PCI_BUS_ID",
		"TO_WHISPER_GPU="+wGPU,
		"TO_NLLB_GPU="+nGPU,
		"TO_WEIGHTS_ROOT="+weightsDir,
	)
	log.Printf("engine GPU env: CUDA_DEVICE_ORDER=PCI_BUS_ID whisper=%q nllb=%q (restart app after changing)",
		wGPU, nGPU)
	cmd.Stdout = lf
	cmd.Stderr = lf
	osproc.Hide(cmd)
	if err := cmd.Start(); err != nil {
		_ = lf.Close()
		return err
	}
	log.Printf("engine subprocess started (pid %d); log %s", cmd.Process.Pid, logPath)
	timing.RecordManagedEngineStart(cmd.Process.Pid)
	setManagedCmd(cmd)
	go func() {
		_ = cmd.Wait()
		_ = lf.Close()
		clearManagedCmd(cmd)
	}()
	return nil
}

func waitEngineHealthy(ctx context.Context, base string, maxWait time.Duration, sink *statusSink) error {
	sink.setPhase(PhaseWaitHealth, "Waiting for engine", -1)
	deadline := time.Now().Add(maxWait)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				timing.UpdateEngineMetaFromHealth(body)
				ok, title, idErr := ProbeEngineIdentity(ctx, base, client)
				if idErr == nil && ok {
					log.Printf("engine identity ok at %s", base)
					return nil
				}
				if idErr == nil && !ok {
					log.Printf("engine at %s is %q (waiting for translation-overlay-engine…)", base, title)
				}
			}
		}
		time.Sleep(400 * time.Millisecond)
	}
	return fmt.Errorf("engine did not become healthy at %s — see %%LOCALAPPDATA%%\\translation-overlay\\engine.log (another app may be using this port)", base)
}

func EngineURL() string {
	if u := strings.TrimSpace(os.Getenv("TO_ENGINE")); u != "" {
		return strings.TrimRight(u, "/")
	}
	return managedEngineBaseURL()
}

func engineURL() string { return EngineURL() }

func Prepare(ctx context.Context, load func() (domain.Settings, error)) error {
	settingsLoader = load
	return prepareEngine(ctx)
}

func LoadModelsWithOptions(ctx context.Context, opts LoadOptions) error {
	return TriggerEngineLoadWithOptions(ctx, EngineURL(), opts)
}

func managedEngineBaseURL() string {
	host := strings.TrimSpace(os.Getenv("TO_ENGINE_HOST"))
	if host == "" {
		host = "127.0.0.1"
	}
	port := strings.TrimSpace(os.Getenv("TO_ENGINE_PORT"))
	if port == "" {
		port = defaultEnginePort
	}
	return "http://" + host + ":" + port
}

func useManagedEngine() bool {
	if strings.TrimSpace(os.Getenv("TO_SKIP_MANAGED_ENGINE")) != "" {
		return false
	}
	if strings.TrimSpace(os.Getenv("TO_ENGINE")) != "" {
		return false
	}
	return true
}

func Shutdown() {
	stopManagedEngine()
}

func portFromBaseURL(base string) string {
	base = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(base), "http://"), "https://")
	if i := strings.LastIndex(base, ":"); i >= 0 {
		return base[i+1:]
	}
	return defaultEnginePort
}
