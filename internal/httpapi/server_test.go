package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"translation-overlay/internal/composition"
)

func TestServeUIHub(t *testing.T) {
	dir := t.TempDir()
	app, err := composition.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Mount(mux, app)

	req := httptest.NewRequest(http.MethodGet, "/ui/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/: status %d body %q", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), "Penguin Translate") {
		t.Fatalf("expected hub HTML, got %d bytes", rec.Body.Len())
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && stringIndex(s, sub) >= 0)
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
