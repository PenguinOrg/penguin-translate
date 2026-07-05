// Package livetranslate bridges the browser microphone to Google's Gemini Live
// Translate model (gemini-3.5-live-translate-preview): a single WebSocket that
// streams raw PCM up and receives translated speech + source/target transcripts
// down, as a continuous simultaneous-interpretation stream (no turn-taking).
//
// A Session owns one upstream Gemini connection and hides connection recycling:
// Gemini drops the socket roughly every ten minutes, so on an unexpected close
// the session redials with the last resumption handle and replays the setup
// message. Callers see an uninterrupted stream of Events.
package livetranslate

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const geminiWSHost = "generativelanguage.googleapis.com"

// EventKind tags what a server Event carries.
type EventKind string

const (
	EventReady EventKind = "ready" // setup accepted; the session is translating
	EventSrc   EventKind = "src"   // source-language transcript delta (what was spoken)
	EventDst   EventKind = "dst"   // target-language transcript delta (the translation)
	EventAudio EventKind = "audio" // translated speech, 24 kHz mono PCM16
	EventError EventKind = "error" // fatal; the session is finished
)

type Event struct {
	Kind EventKind
	Text string
	PCM  []byte
}

type sessionConfig struct {
	apiKey string
	model  string
	target string // BCP-47 target language code (already Gemini-normalized)
	echo   bool
}

// Session is a live translation stream. Feed pushes microphone PCM; Events yields
// transcripts and translated audio until the session is closed or errors.
type Session struct {
	cfg    sessionConfig
	events chan Event

	connMu sync.Mutex // guards conn + all writes to it (gorilla allows one writer)
	conn   *websocket.Conn
	handle string // latest session-resumption handle, replayed on reconnect

	closeOnce sync.Once
	closed    chan struct{}
}

// Dial opens a Gemini live-translate session and blocks until the setup handshake
// completes (or fails). On success a reader goroutine is running and Events is live.
func Dial(ctx context.Context, cfg sessionConfig) (*Session, error) {
	if strings.TrimSpace(cfg.apiKey) == "" {
		return nil, errors.New("gemini api key not configured")
	}
	if strings.TrimSpace(cfg.model) == "" {
		cfg.model = "gemini-3.5-live-translate-preview"
	}
	if strings.TrimSpace(cfg.target) == "" {
		return nil, errors.New("target language not set")
	}
	s := &Session{
		cfg:    cfg,
		events: make(chan Event, 64),
		closed: make(chan struct{}),
	}
	if err := s.connect(ctx); err != nil {
		return nil, err
	}
	go s.readLoop()
	return s, nil
}

// Events is the read side of the server stream. It is closed when the session ends.
func (s *Session) Events() <-chan Event { return s.events }

// Feed forwards a chunk of 16 kHz mono PCM16 microphone audio to the model.
func (s *Session) Feed(pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	msg := map[string]any{
		"realtimeInput": map[string]any{
			"audio": map[string]any{
				"data":     base64.StdEncoding.EncodeToString(pcm),
				"mimeType": "audio/pcm;rate=16000",
			},
		},
	}
	return s.writeJSON(msg)
}

// Close ends the session and releases the upstream connection.
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		close(s.closed)
		s.connMu.Lock()
		if s.conn != nil {
			_ = s.conn.Close()
		}
		s.connMu.Unlock()
	})
}

func (s *Session) writeJSON(v any) error {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.conn == nil {
		return errors.New("session not connected")
	}
	return s.conn.WriteJSON(v)
}

// connect dials Gemini, sends the setup message, and waits for setupComplete. It
// swaps in the new connection under connMu so a concurrent Feed can't write to a
// half-open socket during reconnect.
func (s *Session) connect(ctx context.Context) error {
	url := fmt.Sprintf("wss://%s/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent?key=%s",
		geminiWSHost, s.cfg.apiKey)

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 15 * time.Second
	conn, _, err := dialer.DialContext(ctx, url, http.Header{})
	if err != nil {
		return fmt.Errorf("dial gemini live: %w", err)
	}
	// Server audio frames are large base64 blobs; the gorilla default (no limit)
	// is fine, but be explicit so a future default change can't truncate them.
	conn.SetReadLimit(8 << 20)

	generationConfig := map[string]any{
		"responseModalities": []string{"AUDIO"},
		"translationConfig": map[string]any{
			"targetLanguageCode": s.cfg.target,
			"echoTargetLanguage": s.cfg.echo,
		},
	}
	setup := map[string]any{
		"model":                    "models/" + s.cfg.model,
		"generationConfig":         generationConfig,
		"inputAudioTranscription":  map[string]any{},
		"outputAudioTranscription": map[string]any{},
		"sessionResumption":        s.resumptionConfig(),
	}
	if err := conn.WriteJSON(map[string]any{"setup": setup}); err != nil {
		_ = conn.Close()
		return fmt.Errorf("gemini setup: %w", err)
	}

	// Block for setupComplete so Dial only returns once the model is translating.
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	for {
		var raw map[string]json.RawMessage
		if err := conn.ReadJSON(&raw); err != nil {
			_ = conn.Close()
			return fmt.Errorf("gemini setup read: %w", err)
		}
		if _, ok := raw["setupComplete"]; ok {
			break
		}
		if e, ok := raw["error"]; ok {
			_ = conn.Close()
			return fmt.Errorf("gemini setup rejected: %s", string(e))
		}
	}
	_ = conn.SetReadDeadline(time.Time{})

	s.connMu.Lock()
	s.conn = conn
	s.connMu.Unlock()
	return nil
}

func (s *Session) resumptionConfig() map[string]any {
	s.connMu.Lock()
	h := s.handle
	s.connMu.Unlock()
	if h == "" {
		return map[string]any{}
	}
	return map[string]any{"handle": h}
}

// readLoop pumps server messages into Events, transparently reconnecting when the
// upstream connection recycles. It exits when the session is closed or a
// reconnect fails.
func (s *Session) readLoop() {
	defer close(s.events)
	first := true
	for {
		select {
		case <-s.closed:
			return
		default:
		}

		s.connMu.Lock()
		conn := s.conn
		s.connMu.Unlock()
		if conn == nil {
			return
		}
		if first {
			s.emit(Event{Kind: EventReady})
			first = false
		}

		reconnect := false
		for {
			var raw map[string]json.RawMessage
			if err := conn.ReadJSON(&raw); err != nil {
				select {
				case <-s.closed:
					return
				default:
					reconnect = true
				}
				break
			}
			if done := s.dispatch(raw); done {
				// A GoAway or terminal signal: drop this socket and redial.
				reconnect = true
				break
			}
		}

		if !reconnect {
			return
		}
		if !s.reconnect() {
			s.emit(Event{Kind: EventError, Text: "live translation connection lost"})
			return
		}
	}
}

// dispatch parses one server message into Events and reports whether the caller
// should tear the socket down (GoAway).
func (s *Session) dispatch(raw map[string]json.RawMessage) (goAway bool) {
	if u, ok := raw["sessionResumptionUpdate"]; ok {
		var upd struct {
			NewHandle string `json:"newHandle"`
			Resumable bool   `json:"resumable"`
		}
		if json.Unmarshal(u, &upd) == nil && upd.NewHandle != "" {
			s.connMu.Lock()
			s.handle = upd.NewHandle
			s.connMu.Unlock()
		}
	}
	if _, ok := raw["goAway"]; ok {
		goAway = true
	}
	sc, ok := raw["serverContent"]
	if !ok {
		return goAway
	}
	var content struct {
		InputTranscription  *struct{ Text string } `json:"inputTranscription"`
		OutputTranscription *struct{ Text string } `json:"outputTranscription"`
		ModelTurn           *struct {
			Parts []struct {
				InlineData *struct {
					Data string `json:"data"`
				} `json:"inlineData"`
			} `json:"parts"`
		} `json:"modelTurn"`
	}
	if err := json.Unmarshal(sc, &content); err != nil {
		return goAway
	}
	if content.InputTranscription != nil && content.InputTranscription.Text != "" {
		s.emit(Event{Kind: EventSrc, Text: content.InputTranscription.Text})
	}
	if content.OutputTranscription != nil && content.OutputTranscription.Text != "" {
		s.emit(Event{Kind: EventDst, Text: content.OutputTranscription.Text})
	}
	if content.ModelTurn != nil {
		for _, p := range content.ModelTurn.Parts {
			if p.InlineData == nil || p.InlineData.Data == "" {
				continue
			}
			if pcm, err := base64.StdEncoding.DecodeString(p.InlineData.Data); err == nil && len(pcm) > 0 {
				s.emit(Event{Kind: EventAudio, PCM: pcm})
			}
		}
	}
	return goAway
}

func (s *Session) reconnect() bool {
	s.connMu.Lock()
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
	s.connMu.Unlock()

	// A couple of quick attempts covers the routine ~10-minute recycle; the
	// resumption handle carries context across the gap.
	for attempt := 0; attempt < 3; attempt++ {
		select {
		case <-s.closed:
			return false
		case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := s.connect(ctx)
		cancel()
		if err == nil {
			return true
		}
	}
	return false
}

func (s *Session) emit(ev Event) {
	select {
	case s.events <- ev:
	case <-s.closed:
	}
}
