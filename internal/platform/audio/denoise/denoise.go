package denoise

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"translation-overlay/internal/platform/denoisebinary"
	"translation-overlay/internal/platform/lifecycle"
	"translation-overlay/internal/platform/osproc"
)

const (
	exeFileName  = "penguin-translate-denoise.exe"
	maxWavBytes  = 256 << 20
	callDeadline = 10 * time.Second
)

var ErrUnavailable = errors.New("denoise: sidecar unavailable")

type manager struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

var mgr = &manager{}

func init() {
	lifecycle.Register(Stop)
}

func exePath() (string, bool) {
	if p := strings.TrimSpace(os.Getenv("TO_DENOISE_EXE")); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	if p, err := denoisebinary.Materialize(); err == nil {
		return p, true
	}
	if exe, err := os.Executable(); err == nil {
		sib := filepath.Join(filepath.Dir(exe), exeFileName)
		if _, err := os.Stat(sib); err == nil {
			return sib, true
		}
	}
	return "", false
}

func Available() bool {
	_, ok := exePath()
	return ok
}

func Status() string {
	if Available() {
		return "rnnoise"
	}
	return "unavailable"
}

// Denoise must fail open: every error path returns the original wav so a denoise
// problem degrades to passthrough audio and never drops a caption.
func Denoise(wav []byte) ([]byte, error) {
	if len(wav) == 0 || len(wav) > maxWavBytes {
		return wav, fmt.Errorf("denoise: invalid input size %d", len(wav))
	}
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	out, err := mgr.roundtripLocked(wav)
	if err != nil {
		mgr.stopLocked()
		out, err = mgr.roundtripLocked(wav)
		if err != nil {
			mgr.stopLocked()
			return wav, err
		}
	}
	return out, nil
}

var debugCount int64

func DebugDir() string {
	if d := strings.TrimSpace(os.Getenv("TO_DENOISE_DUMP_DIR")); d != "" {
		return d
	}
	if d := strings.TrimSpace(os.Getenv("TO_DATA_DIR")); d != "" {
		return filepath.Join(d, "denoise-debug")
	}
	if runtime.GOOS == "windows" {
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			return filepath.Join(local, "translation-overlay", "denoise-debug")
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "translation-overlay", "denoise-debug")
	}
	return ""
}

func DebugCount() int64 { return atomic.LoadInt64(&debugCount) }

func WriteDebugChunk(raw, denoised []byte, meta map[string]any) {
	dir := DebugDir()
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	n := atomic.AddInt64(&debugCount, 1)
	base := filepath.Join(dir, fmt.Sprintf("seg-%s-%04d", time.Now().Format("20060102-150405.000"), n))
	if len(raw) > 0 {
		_ = os.WriteFile(base+"-raw.wav", raw, 0o644)
	}
	if len(denoised) > 0 {
		_ = os.WriteFile(base+"-denoised.wav", denoised, 0o644)
	}
	if meta != nil {
		if b, err := json.MarshalIndent(meta, "", "  "); err == nil {
			_ = os.WriteFile(base+".json", b, 0o644)
		}
	}
}

func Stop() {
	mgr.mu.Lock()
	mgr.stopLocked()
	mgr.mu.Unlock()
}

func (m *manager) startLocked() error {
	if m.cmd != nil {
		return nil
	}
	path, ok := exePath()
	if !ok {
		return ErrUnavailable
	}
	cmd := exec.Command(path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return err
	}
	cmd.Stderr = io.Discard
	osproc.Hide(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	m.cmd = cmd
	m.stdin = stdin
	m.stdout = stdout
	return nil
}

func (m *manager) stopLocked() {
	if m.cmd == nil {
		return
	}
	if m.stdin != nil {
		_ = m.stdin.Close()
	}
	if m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
	}
	_ = m.cmd.Wait()
	m.cmd = nil
	m.stdin = nil
	m.stdout = nil
}

func (m *manager) roundtripLocked(wav []byte) ([]byte, error) {
	if err := m.startLocked(); err != nil {
		return nil, err
	}
	stdin, stdout := m.stdin, m.stdout

	type result struct {
		buf []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		var hdr [4]byte
		binary.LittleEndian.PutUint32(hdr[:], uint32(len(wav)))
		if _, err := stdin.Write(hdr[:]); err != nil {
			done <- result{nil, err}
			return
		}
		if _, err := stdin.Write(wav); err != nil {
			done <- result{nil, err}
			return
		}
		if _, err := io.ReadFull(stdout, hdr[:]); err != nil {
			done <- result{nil, err}
			return
		}
		n := binary.LittleEndian.Uint32(hdr[:])
		if n == 0 || int(n) > maxWavBytes {
			done <- result{nil, fmt.Errorf("denoise: bad reply length %d", n)}
			return
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(stdout, buf); err != nil {
			done <- result{nil, err}
			return
		}
		done <- result{buf, nil}
	}()

	select {
	case r := <-done:
		return r.buf, r.err
	case <-time.After(callDeadline):
		return nil, errors.New("denoise: timed out")
	}
}
