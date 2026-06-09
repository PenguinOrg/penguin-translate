package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"translation-overlay/internal/composition"
)

func newTestMux(t *testing.T) *http.ServeMux {
	t.Helper()
	app, err := composition.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Mount(mux, app)
	return mux
}

func getSettings(t *testing.T, mux *http.ServeMux) map[string]any {
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

func postSettings(t *testing.T, mux *http.ServeMux, body string) map[string]any {
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
