package livetranslate

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"translation-overlay/internal/platform/domain"
)

// stubRepo is a minimal in-memory SettingsRepository holding one Settings value.
type stubRepo struct{ st domain.Settings }

func (r *stubRepo) Load() (domain.Settings, error) { return r.st, nil }
func (r *stubRepo) Save(s domain.Settings) error   { r.st = s; return nil }
func (r *stubRepo) Update(mutate func(*domain.Settings) error) (domain.Settings, error) {
	if err := mutate(&r.st); err != nil {
		return r.st, err
	}
	return r.st, nil
}
func (r *stubRepo) Path() string { return "" }
func (r *stubRepo) Exists() bool { return true }

// TestBridgeTranslatesRealAudio drives the full browser->Go->Gemini bridge against
// the live gemini-3.5-live-translate-preview model, streaming a synthesized English
// WAV and asserting Japanese transcript + translated audio come back.
//
// Termination is deliberate, not idle-based: the model streams continuous silence
// audio between utterances, so waiting for "the audio to stop" never returns. We
// stream the clip, grant a fixed grace window for the final text deltas, then send
// stop and assert on what arrived.
//
// Gated on GEMINI_API_KEY (+ LIVE_TRANSLATE_WAV pointing at a 16 kHz mono PCM16
// WAV); skips otherwise so CI without a key/network stays green.
func TestBridgeTranslatesRealAudio(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	wavPath := strings.TrimSpace(os.Getenv("LIVE_TRANSLATE_WAV"))
	if key == "" || wavPath == "" {
		t.Skip("set GEMINI_API_KEY and LIVE_TRANSLATE_WAV to run the live bridge test")
	}
	pcm, err := readWavPCM(wavPath)
	if err != nil {
		t.Fatalf("read wav: %v", err)
	}

	repo := &stubRepo{st: domain.DefaultSettings("")}
	repo.st.GeminiAPIKey = key
	host := New(repo)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/live/mic", host.HandleMicWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/live/mic"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": {srv.URL}})
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	defer conn.Close()

	var (
		mu                sync.Mutex
		src, dst, lastErr string
		audioLen          int
		gotReady          bool
	)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			mu.Lock()
			if mt == websocket.BinaryMessage {
				audioLen += len(msg)
			} else {
				var ev struct{ Kind, Text, Msg string }
				_ = json.Unmarshal(msg, &ev)
				switch ev.Kind {
				case "ready":
					gotReady = true
				case "src":
					src += ev.Text
				case "dst":
					dst += ev.Text
				case "error":
					lastErr = ev.Msg
				}
			}
			mu.Unlock()
		}
	}()

	if err := conn.WriteJSON(map[string]any{"cmd": "start", "target": "ja", "echo": false}); err != nil {
		t.Fatalf("send start: %v", err)
	}

	// Stream the WAV as 100 ms (3200-byte) frames at ~real time.
	const frame = 3200
	for off := 0; off < len(pcm); off += frame {
		end := min(off+frame, len(pcm))
		if err := conn.WriteMessage(websocket.BinaryMessage, pcm[off:end]); err != nil {
			t.Fatalf("send audio: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Grace window for trailing transcript deltas, then end the session.
	time.Sleep(7 * time.Second)
	_ = conn.WriteJSON(map[string]any{"cmd": "stop"})
	_ = conn.Close()
	select {
	case <-readerDone:
	case <-time.After(3 * time.Second):
	}

	mu.Lock()
	defer mu.Unlock()
	t.Logf("ready=%v src=%q dst=%q audioBytes=%d err=%q", gotReady, src, dst, audioLen, lastErr)
	if lastErr != "" {
		t.Fatalf("bridge reported error: %s", lastErr)
	}
	if !gotReady {
		t.Fatalf("never received ready event")
	}
	if strings.TrimSpace(dst) == "" {
		t.Fatalf("no translated transcript received")
	}
	if audioLen == 0 {
		t.Fatalf("no translated audio received")
	}
}

// readWavPCM returns the PCM payload of a RIFF/WAVE file (locates the data chunk).
func readWavPCM(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	idx := bytes.Index(raw, []byte("data"))
	if idx < 0 || idx+8 > len(raw) {
		return raw, nil
	}
	size := binary.LittleEndian.Uint32(raw[idx+4 : idx+8])
	start := idx + 8
	end := min(start+int(size), len(raw))
	return raw[start:end], nil
}
