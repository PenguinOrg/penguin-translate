package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"translation-overlay/internal/composition"
)

func newTestMux(t *testing.T) http.Handler {
	t.Helper()
	app, err := composition.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Mount(http.NewServeMux(), app)
}

func getSettings(t *testing.T, mux http.Handler) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/settings: status %d body %q", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	return out
}

func postSettings(t *testing.T, mux http.Handler, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/settings %s: status %d body %q", body, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	return out
}

func section(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	s, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("missing %q section in %v", key, m)
	}
	return s
}

func TestUnifiedSettingsShape(t *testing.T) {
	mux := newTestMux(t)
	st := getSettings(t, mux)

	for _, k := range []string{"openai_key_configured", "openrouter_key_configured", "openai_base_url", "skip_words"} {
		if _, ok := st[k]; !ok {
			t.Errorf("GET missing top-level key %q", k)
		}
	}
	if _, ok := section(t, st, "practice")["score_threshold"].(float64); !ok {
		t.Errorf("practice section missing numeric score_threshold")
	}
	if _, ok := section(t, st, "audio")["segment_timeout_sec"].(float64); !ok {
		t.Errorf("audio section missing numeric segment_timeout_sec")
	}
}

func TestUnifiedSettingsCredentials(t *testing.T) {
	mux := newTestMux(t)

	st := postSettings(t, mux, `{"openai_api_key":"sk-test","openai_base_url":"https://example.test"}`)
	if st["openai_key_configured"] != true {
		t.Fatalf("after key save, configured = %v, want true", st["openai_key_configured"])
	}
	if st["openai_base_url"] != "https://example.test" {
		t.Fatalf("base url = %v, want https://example.test", st["openai_base_url"])
	}

	st = postSettings(t, mux, `{"practice":{"score_threshold":80}}`)
	if st["openai_key_configured"] != true {
		t.Errorf("section POST cleared openai key (configured=%v)", st["openai_key_configured"])
	}
	if st["openai_base_url"] != "https://example.test" {
		t.Errorf("section POST cleared openai_base_url = %v", st["openai_base_url"])
	}
	if section(t, st, "practice")["score_threshold"].(float64) != 80 {
		t.Errorf("score_threshold not saved")
	}

	st = postSettings(t, mux, `{"remove_openai_key":true}`)
	if st["openai_key_configured"] != false {
		t.Errorf("after removal, configured = %v, want false", st["openai_key_configured"])
	}
}

func TestUnifiedSettingsDeepgram(t *testing.T) {
	mux := newTestMux(t)

	st := postSettings(t, mux, `{"deepgram_api_key":"dgk","audio":{"api_provider":"deepgram","translate_provider":"openai","pipeline_mode":"multimodal"}}`)
	if st["deepgram_key_configured"] != true {
		t.Fatalf("deepgram_key_configured = %v, want true", st["deepgram_key_configured"])
	}
	if st["deepgram_base_url"] != "https://api.deepgram.com" {
		t.Errorf("deepgram_base_url = %v, want default", st["deepgram_base_url"])
	}
	a := section(t, st, "audio")
	if a["api_provider"] != "deepgram" {
		t.Errorf("api_provider = %v, want deepgram", a["api_provider"])
	}
	if a["translate_provider"] != "openai" {
		t.Errorf("translate_provider = %v, want openai", a["translate_provider"])
	}
	if a["pipeline_mode"] != "split" {
		t.Errorf("pipeline_mode = %v, want split (Deepgram is ASR/split-only)", a["pipeline_mode"])
	}

	st = postSettings(t, mux, `{"remove_deepgram_key":true}`)
	if st["deepgram_key_configured"] != false {
		t.Errorf("after removal, deepgram_key_configured = %v, want false", st["deepgram_key_configured"])
	}
}

func TestUnifiedSettingsPartialSectionUpdate(t *testing.T) {
	mux := newTestMux(t)

	before := section(t, getSettings(t, mux), "practice")["target_language"]
	st := postSettings(t, mux, `{"practice":{"score_threshold":70}}`)
	p := section(t, st, "practice")
	if p["score_threshold"].(float64) != 70 {
		t.Errorf("score_threshold = %v, want 70", p["score_threshold"])
	}
	if p["target_language"] != before {
		t.Errorf("partial update clobbered target_language: %v -> %v", before, p["target_language"])
	}

	st = postSettings(t, mux, `{"audio":{"segment_timeout_sec":9}}`)
	if section(t, st, "audio")["segment_timeout_sec"].(float64) != 9 {
		t.Errorf("audio segment_timeout_sec not saved")
	}
	if section(t, st, "practice")["score_threshold"].(float64) != 70 {
		t.Errorf("audio POST disturbed practice score_threshold = %v", section(t, st, "practice")["score_threshold"])
	}
}

func TestSettingsResponseOmitsRawKeys(t *testing.T) {
	mux := newTestMux(t)
	secrets := []string{"sk-raw-openai", "sk-or-raw-openrouter", "raw-dashscope-key", "raw-deepgram-key", "raw-azure-key"}
	postSettings(t, mux, `{"openai_api_key":"sk-raw-openai","openrouter_api_key":"sk-or-raw-openrouter","dashscope_api_key":"raw-dashscope-key","deepgram_api_key":"raw-deepgram-key","azure_speech_key":"raw-azure-key"}`)

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/settings: status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, secret := range secrets {
		if strings.Contains(body, secret) {
			t.Errorf("GET /api/settings leaks stored key %q", secret)
		}
	}

	st := getSettings(t, mux)
	for _, flag := range []string{"openai_key_configured", "openrouter_key_configured", "dashscope_key_configured", "deepgram_key_configured", "azure_key_configured"} {
		if st[flag] != true {
			t.Errorf("%s = %v, want true", flag, st[flag])
		}
	}
	for _, m := range []map[string]any{st, section(t, st, "practice"), section(t, st, "audio")} {
		for _, k := range []string{"openai_api_key", "openrouter_api_key", "dashscope_api_key", "deepgram_api_key", "azure_speech_key"} {
			if _, ok := m[k]; ok {
				t.Errorf("settings response still carries raw key field %q", k)
			}
		}
	}
}

func TestSettingsFailClosedWhenLoadFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	corrupt := []byte(`{"openai_api_key":`)
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := composition.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	mux := Mount(http.NewServeMux(), app)

	req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(`{"openai_api_key":"sk-new"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("POST with unreadable settings: status %d, want 500 (no default fallback)", rec.Code)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(corrupt) {
		t.Fatal("failed POST rewrote settings.json")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET with unreadable settings: status %d, want 500", rec.Code)
	}
}

func TestSettingsRejectsForeignBrowserOrigin(t *testing.T) {
	mux := newTestMux(t)
	cases := []struct {
		origin string
		want   int
	}{
		{"", http.StatusOK},
		{"http://127.0.0.1:18780", http.StatusOK},
		{"http://localhost:18780", http.StatusOK},
		{"http://wails.localhost", http.StatusOK},
		{"https://evil.example", http.StatusForbidden},
		{"null", http.StatusForbidden},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(`{}`))
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("POST /api/settings Origin=%q: status %d, want %d", c.origin, rec.Code, c.want)
		}
	}
}
