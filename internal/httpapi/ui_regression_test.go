package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"translation-overlay/internal/composition"
)

func TestServesRestoredLatencyPanel(t *testing.T) {
	mux := newTestMux(t)
	req := httptest.NewRequest(http.MethodGet, "/ui/overlay-timings.html", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/overlay-timings.html: status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Pipeline latency", "/api/overlay/timings", "/api/wt/timings"} {
		if !strings.Contains(body, want) {
			t.Errorf("restored latency panel missing %q", want)
		}
	}
}

func TestOutputLanguageRoundTrips(t *testing.T) {
	mux := newTestMux(t)
	postSettings(t, mux, `{"practice":{"my_language":"en","other_languages":["zh"]}}`)
	p := section(t, getSettings(t, mux), "practice")
	others, _ := p["other_languages"].([]any)
	if len(others) != 1 || others[0] != "zh" {
		t.Fatalf("other_languages not durable as [zh]: %v", p["other_languages"])
	}
}

func TestUpgradeKeepsPriorLanguage(t *testing.T) {
	dir := t.TempDir()
	old := `{"practice":{"target_language":"jp"},"audio":{"primary_language":"zh"}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := composition.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Mount(mux, app)

	others, _ := section(t, getSettings(t, mux), "practice")["other_languages"].([]any)
	if len(others) != 1 || others[0] != "zh" {
		t.Fatalf("upgrade dropped the user's language: other_languages = %v, want [zh]", others)
	}
}

func TestVRChatOscConfigRoundTrips(t *testing.T) {
	mux := newTestMux(t)
	postSettings(t, mux, `{"practice":{"plugins":{"vrchat_osc":{"enabled":true,"include_original":true,"port":9001}}}}`)
	p := section(t, getSettings(t, mux), "practice")
	plugins, _ := p["plugins"].(map[string]any)
	osc, _ := plugins["vrchat_osc"].(map[string]any)
	if osc == nil {
		t.Fatal("vrchat_osc plugin config missing from settings")
	}
	if osc["enabled"] != true {
		t.Errorf("enabled not applied: %v", osc["enabled"])
	}
	if osc["include_original"] != true {
		t.Errorf("include_original not applied: %v", osc["include_original"])
	}
	if port, _ := osc["port"].(float64); port != 9001 {
		t.Errorf("port not applied: %v", osc["port"])
	}
}
