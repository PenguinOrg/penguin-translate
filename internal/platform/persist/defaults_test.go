package persist

import (
	"path/filepath"
	"testing"
)

func TestDefaultIsCleanAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	repo := NewJSONRepository(path)
	if repo.Exists() {
		t.Fatal("fresh temp dir should not already have settings")
	}
	if err := repo.Save(Default()); err != nil {
		t.Fatal(err)
	}

	st, err := repo.Load()
	if err != nil {
		t.Fatal(err)
	}
	if st.OpenAIAPIKey != "" || st.OpenRouterAPIKey != "" {
		t.Fatalf("fresh install inherited API keys: openai=%q openrouter=%q", st.OpenAIAPIKey, st.OpenRouterAPIKey)
	}
	if st.Window.NLLBBaseURL != defaultEngineURL {
		t.Fatalf("nllb base url = %q, want %q", st.Window.NLLBBaseURL, defaultEngineURL)
	}
	if st.OpenRouterBaseURL == "" {
		t.Fatal("openrouter base url should be defaulted on load")
	}
	if st.MicTranslate.Plugins == nil {
		t.Fatal("plugins map should be initialized on load")
	}
}
