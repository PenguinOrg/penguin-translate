package host

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"translation-overlay/internal/feature/mictranslate/infra/plugin"
	"translation-overlay/internal/platform/domain"
)

// commitStubRepo is a minimal in-memory SettingsRepository for the handler test.
type commitStubRepo struct{ st domain.Settings }

func (r *commitStubRepo) Load() (domain.Settings, error) { return r.st, nil }
func (r *commitStubRepo) Save(s domain.Settings) error   { r.st = s; return nil }
func (r *commitStubRepo) Update(mutate func(*domain.Settings) error) (domain.Settings, error) {
	if err := mutate(&r.st); err != nil {
		return r.st, err
	}
	return r.st, nil
}
func (r *commitStubRepo) Path() string { return "" }
func (r *commitStubRepo) Exists() bool { return true }

// TestLiveCommitReachesOSC proves an Interpreter turn committed over
// /api/live/commit is fanned out to the VRChat OSC plugin and lands on the wire
// as a /chatbox/input packet. This is the whole server-side chain the user cares
// about (handler -> plugin bus -> OSC), asserted against a real UDP socket.
func TestLiveCommitReachesOSC(t *testing.T) {
	// Local UDP socket standing in for VRChat's OSC input port.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer pc.Close()
	port := pc.LocalAddr().(*net.UDPAddr).Port

	// Enable the OSC plugin, pointed at our socket. The config must live in the
	// persisted settings: handleLiveCommit calls readSettingsFromDisk, which
	// re-applies plugin configs from settings on every request — so a config set
	// only via ApplyAllConfigs would be wiped back to disabled before the dispatch.
	cfg, _ := json.Marshal(map[string]any{
		"enabled":          true,
		"host":             "127.0.0.1",
		"port":             port,
		"include_original": false,
	})
	st := domain.DefaultSettings("")
	st.MicTranslate.Plugins = map[string]json.RawMessage{"vrchat_osc": cfg}
	repo := &commitStubRepo{st: st}
	// Reset the shared plugin to disabled afterward so this doesn't leak enabled
	// state into other tests in the package.
	defer plugin.Default.ApplyAllConfigs(map[string]json.RawMessage{"vrchat_osc": json.RawMessage(`{}`)})

	h := New(repo)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/live/commit", h.handleLiveCommit)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	const translated = "こんにちは"
	body, _ := json.Marshal(map[string]any{
		"source_text":     "hello",
		"target_language": "ja",
		"target_text":     translated,
	})
	resp, err := http.Post(srv.URL+"/api/live/commit", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post commit: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("commit status = %d", resp.StatusCode)
	}

	// The pacer sends the first queued item immediately; give the datagram a
	// generous window to arrive on loopback.
	_ = pc.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 4096)
	n, _, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("no OSC packet received: %v", err)
	}
	got := buf[:n]
	if !bytes.Contains(got, []byte("/chatbox/input")) {
		t.Fatalf("packet is not a chatbox/input message: %q", got)
	}
	if !strings.Contains(string(got), translated) {
		t.Fatalf("translated text %q not in OSC packet: %q", translated, got)
	}
}
