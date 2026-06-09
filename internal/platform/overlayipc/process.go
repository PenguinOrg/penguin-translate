package overlayipc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"translation-overlay/internal/platform/osproc"
	"translation-overlay/internal/platform/overlaybinary"
)

const exeName = "penguin-translate-overlay.exe"

func ExePath() string {
	if p := strings.TrimSpace(os.Getenv("TO_OVERLAY_EXE")); p != "" {
		return p
	}
	if p, err := overlaybinary.Materialize(); err == nil {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(exe), exeName)
		if _, err := os.Stat(sibling); err == nil {
			return sibling
		}
	}
	return exeName
}

type Config struct {
	Name     string
	Env      []string
	OnStdout func(line string)
	OnStderr func(line string)
}

type Process struct {
	cfg Config

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	running bool
	lastErr string
}

func New(cfg Config) *Process {
	if cfg.Name == "" {
		cfg.Name = "overlay"
	}
	return &Process{cfg: cfg}
}

func (p *Process) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cmd != nil && p.cmd.Process != nil && p.running
}

func (p *Process) LastError() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastErr
}

func (p *Process) Start() (string, error) {
	p.mu.Lock()
	if p.running && p.stdin != nil {
		p.mu.Unlock()
		return "already running", nil
	}
	p.mu.Unlock()

	exe := ExePath()
	if _, err := os.Stat(exe); err != nil {
		msg := fmt.Sprintf("overlay binary missing: %s (rebuild with build/scripts/build.ps1)", exe)
		p.setErr(msg)
		return "", fmt.Errorf("%s", msg)
	}

	cmd := exec.Command(exe)
	if len(p.cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), p.cfg.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	cmd.Stdout = &lineWriter{fn: p.dispatchStdout}
	cmd.Stderr = &lineWriter{fn: p.dispatchStderr}
	osproc.Hide(cmd)
	if err := cmd.Start(); err != nil {
		return "", err
	}

	p.mu.Lock()
	p.cmd = cmd
	p.stdin = stdin
	p.running = true
	p.lastErr = ""
	p.mu.Unlock()

	go func() {
		werr := cmd.Wait()
		p.mu.Lock()
		p.running = false
		p.stdin = nil
		p.cmd = nil
		if werr != nil {
			p.lastErr = werr.Error()
		}
		p.mu.Unlock()
		log.Printf("%s: process exited: %v", p.cfg.Name, werr)
	}()

	log.Printf("%s started pid=%d exe=%s", p.cfg.Name, cmd.Process.Pid, exe)
	return "started", nil
}

func (p *Process) Stop() {
	p.mu.Lock()
	if p.stdin != nil {
		_ = p.writeLocked(map[string]string{"op": "quit"})
		_ = p.stdin.Close()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	p.stdin = nil
	p.cmd = nil
	p.running = false
	p.mu.Unlock()
}

func (p *Process) WriteOp(v any) error {
	done := make(chan error, 1)
	go func() {
		p.mu.Lock()
		err := p.writeLocked(v)
		p.mu.Unlock()
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		p.setErr("overlay stdin write timed out")
		return fmt.Errorf("overlay stdin write timed out")
	}
}

func (p *Process) writeLocked(v any) error {
	if p.stdin == nil {
		return fmt.Errorf("overlay process not running")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := p.stdin.Write(append(b, '\n')); err != nil {
		p.lastErr = err.Error()
		return err
	}
	return nil
}

func (p *Process) setErr(s string) {
	p.mu.Lock()
	p.lastErr = s
	p.mu.Unlock()
	log.Printf("%s: %s", p.cfg.Name, s)
}

func (p *Process) dispatchStdout(line string) {
	if p.cfg.OnStdout != nil {
		p.cfg.OnStdout(line)
		return
	}
	log.Printf("%s: %s", p.cfg.Name, line)
}

func (p *Process) dispatchStderr(line string) {
	if p.cfg.OnStderr != nil {
		p.cfg.OnStderr(line)
		return
	}
	log.Printf("%s[stderr]: %s", p.cfg.Name, line)
}

type lineWriter struct {
	fn  func(string)
	buf []byte
}

func (w *lineWriter) Write(b []byte) (int, error) {
	w.buf = append(w.buf, b...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSpace(string(w.buf[:i]))
		w.buf = w.buf[i+1:]
		if line != "" {
			w.fn(line)
		}
	}
	return len(b), nil
}
