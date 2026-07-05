package livetranslate

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"translation-overlay/internal/platform/lang/languages"
	"translation-overlay/internal/platform/netguard"
)

var micWSUpgrader = websocket.Upgrader{
	CheckOrigin: netguard.AllowBrowserOrigin,
}

// HandleMicWS is the browser side of the bridge. The client opens one WebSocket,
// sends a {"cmd":"start","target","echo"} control message, then streams binary
// 16 kHz PCM16 microphone frames. The bridge holds one Gemini session and relays
// transcripts (JSON) and translated speech (binary 24 kHz PCM16) back down.
//
// It is served on the native loopback sidecar (real HTTP port), not the app mux:
// the Wails assetserver cannot upgrade WebSocket connections.
//
// All access to sess happens on this single read goroutine, so it needs no lock;
// only the browser socket has a concurrent (relay) writer, guarded by writeMu.
func (h *Host) HandleMicWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	conn, err := micWSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("live mic ws upgrade: %v", err)
		return
	}
	defer conn.Close()
	conn.SetReadLimit(1 << 20)

	var writeMu sync.Mutex
	writeText := func(v any) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.WriteJSON(v)
	}
	writeBinary := func(b []byte) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.WriteMessage(websocket.BinaryMessage, b)
	}

	var (
		sess    *Session
		relayWG sync.WaitGroup
	)
	stopSession := func() {
		if sess == nil {
			return
		}
		sess.Close()   // closes the upstream conn → readLoop → closes Events
		relayWG.Wait() // let the relay goroutine drain and exit
		sess = nil
	}
	defer stopSession()

	for {
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if mt == websocket.BinaryMessage {
			if sess != nil {
				if err := sess.Feed(msg); err != nil {
					writeText(errEvent(err.Error()))
				}
			}
			continue
		}

		var cmd struct {
			Cmd    string `json:"cmd"`
			Target string `json:"target"`
			Echo   bool   `json:"echo"`
		}
		if json.Unmarshal(msg, &cmd) != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(cmd.Cmd)) {
		case "start":
			stopSession()
			key, model, echoDefault := h.creds()
			if key == "" {
				writeText(errEvent("Gemini API key not set — add it in Settings → Microphone."))
				continue
			}
			target := languages.GeminiCode(cmd.Target)
			echo := cmd.Echo || echoDefault
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			ns, err := Dial(ctx, sessionConfig{apiKey: key, model: model, target: target, echo: echo})
			cancel()
			if err != nil {
				writeText(errEvent(err.Error()))
				continue
			}
			sess = ns
			relayWG.Add(1)
			go func(s *Session) {
				defer relayWG.Done()
				for ev := range s.Events() {
					switch ev.Kind {
					case EventAudio:
						writeBinary(ev.PCM)
					case EventError:
						writeText(errEvent(ev.Text))
					default:
						writeText(map[string]any{"kind": string(ev.Kind), "text": ev.Text})
					}
				}
			}(ns)
		case "stop":
			stopSession()
			writeText(map[string]any{"kind": "stopped"})
		}
	}
}

func errEvent(msg string) map[string]any {
	return map[string]any{"kind": string(EventError), "msg": msg}
}
