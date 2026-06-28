package host

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"translation-overlay/internal/platform/domain"
	"translation-overlay/internal/platform/persist"
)

// sineWAV builds a 16 kHz mono PCM16 WAV loud enough to clear IsAudioTooQuiet.
func sineWAV(samples int) []byte {
	var body bytes.Buffer
	for i := range samples {
		v := int16(8000 * math.Sin(2*math.Pi*220*float64(i)/16000))
		_ = binary.Write(&body, binary.LittleEndian, v)
	}
	data := body.Bytes()
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+len(data)))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))     // PCM
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))     // mono
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16000)) // sample rate
	_ = binary.Write(&buf, binary.LittleEndian, uint32(32000)) // byte rate
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))     // block align
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))    // bits/sample
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(data)))
	buf.Write(data)
	return buf.Bytes()
}

// TestTranscribeSegmentEndpointDeepgram drives the real /api/transcribe-segment
// HTTP route with Deepgram as the caption provider and OpenAI as the translate
// provider, both pointed at mock servers.
func TestTranscribeSegmentEndpointDeepgram(t *testing.T) {
	var dgPath, dgAuth string
	dg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dgPath = r.URL.Path
		dgAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"results":{"channels":[{"alternatives":[{"transcript":"これはテストです"}]}]}}`)
	}))
	defer dg.Close()

	var chatHit bool
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chatHit = true
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"[{\"i\":0,\"en\":\"This is a test\"}]"}}]}`)
	}))
	defer chat.Close()

	repo := persist.NewJSONRepository(filepath.Join(t.TempDir(), "settings.json"))
	st := domain.DefaultSettings("http://127.0.0.1:8745")
	st.Audio.APIProvider = "deepgram"
	st.Audio.TranslateProvider = "openai"
	st.Audio.TranscribeModel = "nova-2"
	st.Audio.TranslateModel = "gpt-4o-mini"
	st.Audio.PrimaryLanguage = "ja"
	st.DeepgramAPIKey = "dgk"
	st.DeepgramBaseURL = dg.URL
	st.OpenAIAPIKey = "oak"
	st.OpenAIBaseURL = chat.URL
	if err := repo.Save(st); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	New(repo).MountRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("language", "ja")
	_ = mw.WriteField("translate_to_en", "1")
	fw, _ := mw.CreateFormFile("file", "clip.wav")
	_, _ = fw.Write(sineWAV(8000))
	_ = mw.Close()

	resp, err := http.Post(srv.URL+"/api/transcribe-segment", mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}

	var out struct {
		FullText string `json:"full_text"`
		Filtered bool   `json:"filtered"`
		Segments []struct {
			Text    string `json:"text"`
			English string `json:"english"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal %q: %v", body, err)
	}
	if out.Filtered {
		t.Fatalf("segment was filtered: %s", body)
	}
	if dgPath != "/v1/listen" || dgAuth != "Token dgk" {
		t.Errorf("Deepgram not hit correctly: path=%q auth=%q", dgPath, dgAuth)
	}
	if !chatHit {
		t.Error("translate provider (OpenAI mock) was not contacted")
	}
	if out.FullText != "これはテストです" {
		t.Errorf("full_text = %q", out.FullText)
	}
	if len(out.Segments) != 1 || !strings.Contains(out.Segments[0].English, "This is a test") {
		t.Errorf("segments = %+v", out.Segments)
	}
}
