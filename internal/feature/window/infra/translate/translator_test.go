package translate

import (
	"testing"

	"translation-overlay/internal/platform/domain"
)

func TestNewFromSettingsBuildsProviderCreds(t *testing.T) {
	tests := []struct {
		name         string
		backend      string
		openAIBase   string
		openRtrBase  string
		wantProvider string
		wantBase     string
		wantModel    string
	}{
		{"openai default base", "openai", "", "", "openai", "", "oa-model"},
		{"openai custom base", "openai", "https://proxy.test/v1", "", "openai", "https://proxy.test/v1", "oa-model"},
		{"openrouter", "openrouter", "", "https://openrouter.ai/api/v1", "openrouter", "https://openrouter.ai/api/v1", "or-model"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := domain.Settings{
				OpenAIAPIKey:      "oa-key",
				OpenAIBaseURL:     tt.openAIBase,
				OpenRouterAPIKey:  "or-key",
				OpenRouterBaseURL: tt.openRtrBase,
				Window: domain.WindowSettings{
					TranslateBackend: tt.backend,
					OpenAIModel:      "oa-model",
					OpenRouterModel:  "or-model",
				},
			}
			tr := NewFromSettings(st)
			c, ok := tr.(*Client)
			if !ok {
				t.Fatalf("expected *Client, got %T", tr)
			}
			if c.Creds.APIProvider != tt.wantProvider {
				t.Errorf("APIProvider = %q, want %q", c.Creds.APIProvider, tt.wantProvider)
			}
			if c.Model != tt.wantModel {
				t.Errorf("Model = %q, want %q", c.Model, tt.wantModel)
			}
			gotBase := c.Creds.OpenAIBase
			if tt.wantProvider == "openrouter" {
				gotBase = c.Creds.OpenRouterBase
			}
			if gotBase != tt.wantBase {
				t.Errorf("base = %q, want %q", gotBase, tt.wantBase)
			}
		})
	}
}

func TestNewFromSettingsLocalBackend(t *testing.T) {
	for _, backend := range []string{"nllb", "local"} {
		st := domain.Settings{Window: domain.WindowSettings{TranslateBackend: backend}}
		if _, ok := NewFromSettings(st).(*NLLBClient); !ok {
			t.Errorf("backend %q: expected *NLLBClient", backend)
		}
	}
}

func TestNewFromSettingsResolvesTarget(t *testing.T) {
	tests := []struct {
		myLang string
		want   string
	}{
		{"", "en"},
		{"zh", "zh"},
		{"jp", "ja"},
		{"Chinese", "zh"},
	}
	for _, tt := range tests {
		st := domain.Settings{
			MicTranslate: domain.MicTranslateSettings{MyLanguage: tt.myLang},
			Window:       domain.WindowSettings{TranslateBackend: "openai"},
		}
		c, ok := NewFromSettings(st).(*Client)
		if !ok {
			t.Fatalf("my_language=%q: expected *Client", tt.myLang)
		}
		if c.Target != tt.want {
			t.Errorf("my_language=%q: Target = %q, want %q", tt.myLang, c.Target, tt.want)
		}
	}
}

func TestCacheKeyNamespacesTarget(t *testing.T) {
	base := domain.Settings{Window: domain.WindowSettings{TranslateBackend: "openai", OpenAIModel: "gpt-4o"}}
	if got := CacheKey(base); got != "gpt-4o" {
		t.Errorf("English target: CacheKey = %q, want %q", got, "gpt-4o")
	}
	base.MicTranslate.MyLanguage = "zh"
	if got := CacheKey(base); got != "gpt-4o#zh" {
		t.Errorf("Chinese target: CacheKey = %q, want %q", got, "gpt-4o#zh")
	}
}

func TestCacheKeyTracksBackend(t *testing.T) {
	tests := []struct {
		backend string
		model   string
		orModel string
		want    string
	}{
		{"openai", "gpt-4o", "", "gpt-4o"},
		{"openai", "", "", "gpt-4o-mini"},
		{"openrouter", "", "openai/gpt-4o", "openrouter:openai/gpt-4o"},
		{"openrouter", "", "", "openrouter:openai/gpt-4o-mini"},
		{"nllb", "", "", "nllb"},
		{"local", "", "", "nllb"},
	}
	for _, tt := range tests {
		st := domain.Settings{Window: domain.WindowSettings{
			TranslateBackend: tt.backend,
			OpenAIModel:      tt.model,
			OpenRouterModel:  tt.orModel,
		}}
		if got := CacheKey(st); got != tt.want {
			t.Errorf("CacheKey(backend=%q model=%q or=%q) = %q, want %q", tt.backend, tt.model, tt.orModel, got, tt.want)
		}
	}
}
