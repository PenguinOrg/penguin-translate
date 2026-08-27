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
	postSettings(t, mux, `{"practice":{"my_language":"en","other_languages":["yue","fr","ja"]}}`)
	p := section(t, getSettings(t, mux), "practice")
	others, _ := p["other_languages"].([]any)
	want := []string{"yue", "fr", "ja"}
	if len(others) != len(want) {
		t.Fatalf("other_languages = %v, want %v", p["other_languages"], want)
	}
	for i := range want {
		if others[i] != want[i] {
			t.Fatalf("other_languages = %v, want ordered %v", p["other_languages"], want)
		}
	}
}

func TestPracticeSessionStateRoundTrips(t *testing.T) {
	mux := newTestMux(t)
	for _, active := range []bool{true, false} {
		body := `{"practice":{"session_active":false}}`
		if active {
			body = `{"practice":{"session_active":true}}`
		}
		postSettings(t, mux, body)
		got := section(t, getSettings(t, mux), "practice")["session_active"]
		if got != active {
			t.Fatalf("session_active = %v, want %v", got, active)
		}
	}
}

func TestConversationChromeRegressions(t *testing.T) {
	mux := newTestMux(t)
	index := uiAsset(t, mux, "/ui/")
	for _, want := range []string{"draggable=\"true\"", "startLanguageDrag(id, $event)", "finishLanguageDrop(id)"} {
		if !strings.Contains(index, want) {
			t.Errorf("language reorder UI missing %q", want)
		}
	}
	if strings.Contains(index, `id="runStatus"`) || strings.Contains(index, `id="runStatusText"`) {
		t.Error("run bar still contains the redundant listening status label")
	}

	appJS := uiAsset(t, mux, "/ui/app.js")
	for _, want := range []string{"reorderOther(id, targetId, after = false)", "dedupeKey", "maxItems: 6"} {
		if !strings.Contains(appJS, want) {
			t.Errorf("language/toast store missing %q", want)
		}
	}

	conversationJS := uiAsset(t, mux, "/ui/features/conversation.js")
	for _, want := range []string{"persistSessionActive", "practice.session_active"} {
		if !strings.Contains(conversationJS, want) {
			t.Errorf("conversation session restore missing %q", want)
		}
	}
}

func uiAsset(t *testing.T, mux http.Handler, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d", path, rec.Code)
	}
	return rec.Body.String()
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
