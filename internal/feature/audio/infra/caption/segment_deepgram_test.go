package caption

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"translation-overlay/internal/platform/cloudapi"
)

// TestTranscribeSplitDeepgramRoutesTranslateProvider proves the decoupled wiring:
// transcription goes to Deepgram while the English translation goes to the
// separately-chosen chat provider — neither leaks into the other.
func TestTranscribeSplitDeepgramRoutesTranslateProvider(t *testing.T) {
	var dgPath, dgAuth string
	dg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dgPath = r.URL.Path
		dgAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"results":{"channels":[{"alternatives":[{"transcript":"これはテストです"}]}]}}`)
	}))
	defer dg.Close()

	var chatHit bool
	var gotSourceText string
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chatHit = true
		raw, _ := io.ReadAll(r.Body)
		gotSourceText = string(raw)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"[{\"i\":0,\"en\":\"This is a test\"}]"}}]}`)
	}))
	defer chat.Close()

	req := SegmentRequest{
		WAV:               make([]byte, 1000),
		WantTranslate:     true,
		Language:          "ja",
		Pipeline:          "split",
		Provider:          "deepgram",
		TranslateProvider: "openai",
		TranscribeModel:   "nova-2",
		TranslateModel:    "gpt-4o-mini",
		Timeout:           time.Minute,
		Creds: cloudapi.Credentials{
			DeepgramKey:  "dgk",
			DeepgramBase: dg.URL,
			OpenAIKey:    "oak",
			OpenAIBase:   chat.URL,
			APIProvider:  "deepgram",
		},
	}

	resp, err := transcribeSplit(req)
	if err != nil {
		t.Fatal(err)
	}
	if dgPath != "/v1/listen" {
		t.Errorf("transcription did not hit Deepgram: path = %q", dgPath)
	}
	if dgAuth != "Token dgk" {
		t.Errorf("Deepgram auth = %q, want \"Token dgk\"", dgAuth)
	}
	if !chatHit {
		t.Error("translation did not reach the OpenAI chat provider")
	}
	if !strings.Contains(gotSourceText, "これはテストです") {
		t.Errorf("translate request did not carry the transcript: %q", gotSourceText)
	}
	if resp.FullText != "これはテストです" {
		t.Errorf("FullText = %q", resp.FullText)
	}
	if len(resp.Segments) != 1 || resp.Segments[0].English != "This is a test" {
		b, _ := json.Marshal(resp.Segments)
		t.Errorf("segments = %s", b)
	}
}

// TestTranscribeSplitDeepgramNoTranslate confirms Deepgram transcription works
// standalone — no chat provider is contacted when translation is off.
func TestTranscribeSplitDeepgramNoTranslate(t *testing.T) {
	dg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"results":{"channels":[{"alternatives":[{"transcript":"これはテストです"}]}]}}`)
	}))
	defer dg.Close()

	req := SegmentRequest{
		WAV:             make([]byte, 1000),
		WantTranslate:   false,
		Language:        "ja",
		Pipeline:        "split",
		Provider:        "deepgram",
		TranscribeModel: "nova-2",
		Timeout:         time.Minute,
		Creds:           cloudapi.Credentials{DeepgramKey: "dgk", DeepgramBase: dg.URL, APIProvider: "deepgram"},
	}
	resp, err := transcribeSplit(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.FullText != "これはテストです" {
		t.Errorf("FullText = %q", resp.FullText)
	}
}
