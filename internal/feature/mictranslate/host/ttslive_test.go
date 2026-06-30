package host

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"translation-overlay/internal/platform/persist"
)

// End-to-end probe of the real /api/tts handler (settings load -> synth -> WAV
// wrap -> content-type) using a COPY of the local settings.json so real keys are
// exercised without mutating the user's file. Skipped unless TTS_LIVE=1.
//
//	TTS_LIVE=1 go test ./internal/feature/mictranslate/host/ -run TTSEndpointLive -v
func TestTTSEndpointLive(t *testing.T) {
	if os.Getenv("TTS_LIVE") != "1" {
		t.Skip("set TTS_LIVE=1 to run live TTS endpoint probes")
	}
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		t.Fatal("LOCALAPPDATA unset")
	}
	src, err := os.ReadFile(filepath.Join(local, "translation-overlay", "settings.json"))
	if err != nil {
		t.Fatalf("read real settings.json: %v", err)
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	repo := persist.NewJSONRepository(path)
	h := New(repo)

	// The user's real config is a broken experiment (kokoro/Zephyr). Confirm the
	// heal rewrites it to the verified OpenRouter-Gemini default on load.
	healed := h.readSettingsFromDisk()
	t.Logf("healed-from-disk: engine=%s model=%s voice=%s", healed.TTSEngine, healed.OpenAITTSModel, healed.TTSVoiceName)
	{
		req := httptest.NewRequest("POST", "/api/tts", bytes.NewReader([]byte(`{"text":"もう一度お願いします"}`)))
		rec := httptest.NewRecorder()
		h.handlePracticeTTS(rec, req)
		if rec.Code != 200 {
			t.Fatalf("FAIL healed real config: HTTP %d: %s", rec.Code, rec.Body.String())
		}
		t.Logf("OK healed real config -> %s, %d bytes", rec.Header().Get("Content-Type"), rec.Body.Len())
	}

	set := func(engine, model, voice string) {
		st, _ := repo.Load()
		st.MicTranslate.TTSEngine = engine
		st.MicTranslate.OpenAITTSModel = model
		st.MicTranslate.TTSVoiceName = voice
		if err := repo.Save(st); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	cases := []struct{ name, engine, model, voice, wantPrefix string }{
		{"openrouter/gemini", "openrouter", "google/gemini-3.1-flash-tts-preview", "Kore", "RIFF"},
		{"dashscope/qwen", "dashscope", "qwen3-tts-flash", "Cherry", "RIFF"},
		{"deepgram/aura", "deepgram", "aura-2-thalia-en", "", "ID3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			set(c.engine, c.model, c.voice)
			req := httptest.NewRequest("POST", "/api/tts", bytes.NewReader([]byte(`{"text":"もう一度ゆっくりお願いします"}`)))
			rec := httptest.NewRecorder()
			h.handlePracticeTTS(rec, req)
			if rec.Code != 200 {
				t.Fatalf("FAIL %s: HTTP %d: %s", c.name, rec.Code, rec.Body.String())
			}
			body := rec.Body.Bytes()
			ct := rec.Header().Get("Content-Type")
			t.Logf("OK %s -> %s, %d bytes, head=%q", c.name, ct, len(body), string(body[:min(4, len(body))]))
			if len(body) < 1000 {
				t.Errorf("%s: too few bytes (%d)", c.name, len(body))
			}
		})
	}
}
