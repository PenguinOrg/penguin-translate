package host

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"translation-overlay/internal/platform/persist"
)

// Live Azure test. Set AZURE_LIVE=1 and ASSESS_WAV_DIR to a directory
// containing speech.wav and ja.wav.
func TestAzureAssessEndpointLive(t *testing.T) {
	if os.Getenv("AZURE_LIVE") != "1" {
		t.Skip("set AZURE_LIVE=1 to run live Azure assess endpoint probes")
	}
	wavDir := os.Getenv("ASSESS_WAV_DIR")
	if wavDir == "" {
		t.Fatal("set ASSESS_WAV_DIR to a folder of 16kHz mono WAVs (speech.wav, ja.wav)")
	}
	local := os.Getenv("LOCALAPPDATA")
	src, err := os.ReadFile(filepath.Join(local, "translation-overlay", "settings.json"))
	if err != nil {
		t.Fatalf("read real settings.json: %v", err)
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	repo := persist.NewJSONRepository(path)
	st, _ := repo.Load()
	st.MicTranslate.PracticeEnabled = true
	if err := repo.Save(st); err != nil {
		t.Fatalf("save: %v", err)
	}
	h := New(repo)

	cases := []struct {
		name, wav, language, expected string
		wantProsody                   bool
	}{
		{"english", "speech.wav", "en", "The quick brown fox jumps over the lazy dog", true},
		{"japanese", "ja.wav", "ja", "今日はいい天気ですね", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wav, err := os.ReadFile(filepath.Join(wavDir, c.wav))
			if err != nil {
				t.Skipf("missing WAV %s: %v", c.wav, err)
			}
			var buf bytes.Buffer
			mw := multipart.NewWriter(&buf)
			fw, _ := mw.CreateFormFile("file", c.wav)
			_, _ = fw.Write(wav)
			_ = mw.WriteField("language", c.language)
			_ = mw.WriteField("expected", c.expected)
			_ = mw.WriteField("threshold", strconv.Itoa(50))
			_ = mw.Close()

			req := httptest.NewRequest("POST", "/api/assess", &buf)
			req.Header.Set("Content-Type", mw.FormDataContentType())
			rec := httptest.NewRecorder()
			h.handleAssess(rec, req)
			if rec.Code != 200 {
				t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
			}
			var out struct {
				Score          int    `json:"score"`
				Accepted       bool   `json:"accepted"`
				RecognizedText string `json:"recognized_text"`
				Sub            struct {
					Accuracy, Fluency, Completeness, Prosody float64
				} `json:"sub"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("bad JSON: %v — %s", err, rec.Body.String())
			}
			t.Logf("%s -> score=%d recognized=%q acc=%.0f flu=%.0f comp=%.0f pros=%.1f",
				c.name, out.Score, out.RecognizedText, out.Sub.Accuracy, out.Sub.Fluency, out.Sub.Completeness, out.Sub.Prosody)
			if out.Sub.Accuracy <= 0 {
				t.Fatalf("accuracy is 0 — locale %q likely wrong or parse broken", c.language)
			}
			if c.wantProsody && out.Sub.Prosody <= 0 {
				t.Fatalf("expected prosody > 0 for en-US, got %.1f", out.Sub.Prosody)
			}
			if !c.wantProsody && out.Sub.Prosody != 0 {
				t.Logf("note: non-en locale returned prosody %.1f (usually 0)", out.Sub.Prosody)
			}
		})
	}
}
