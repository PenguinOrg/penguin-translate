package host

import (
	"testing"

	"translation-overlay/internal/platform/buildconfig"
	"translation-overlay/internal/platform/domain"
)

func TestNormalizeTranslateProvider(t *testing.T) {
	cases := []struct {
		name         string
		asr          string
		translate    string
		wantASR      string
		wantTrans    string
		wantPipeline string
		pipeline     string
	}{
		{"deepgram defaults translate to openai", "deepgram", "", "deepgram", "openai", "split", "split"},
		{"deepgram keeps explicit translate provider", "deepgram", "openrouter", "deepgram", "openrouter", "split", "split"},
		{"deepgram forced to split even if multimodal requested", "deepgram", "openai", "deepgram", "openai", "split", "multimodal"},
		{"chat provider mirrors itself when translate unset", "dashscope", "", "dashscope", "dashscope", "split", "split"},
		{"openrouter keeps multimodal", "openrouter", "", "openrouter", "openrouter", "multimodal", "multimodal"},
		{"invalid translate provider normalizes to asr", "openai", "deepgram", "openai", "openai", "split", "split"},
		{"penguin mirrors itself when translate unset", "penguin", "", "penguin", "penguin", "split", "split"},
		{"penguin accepted as explicit translate provider", "openai", "penguin", "openai", "penguin", "split", "split"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := normalizeSettings(settingsFile{AudioSettings: domain.AudioSettings{
				APIProvider:       c.asr,
				TranslateProvider: c.translate,
				PipelineMode:      c.pipeline,
			}})
			if got.APIProvider != c.wantASR {
				t.Errorf("api_provider = %q, want %q", got.APIProvider, c.wantASR)
			}
			if got.TranslateProvider != c.wantTrans {
				t.Errorf("translate_provider = %q, want %q", got.TranslateProvider, c.wantTrans)
			}
			if got.PipelineMode != c.wantPipeline {
				t.Errorf("pipeline_mode = %q, want %q", got.PipelineMode, c.wantPipeline)
			}
		})
	}
}

func TestNormalizeDeepgramBaseURL(t *testing.T) {
	got := normalizeSettings(settingsFile{AudioSettings: domain.AudioSettings{APIProvider: "deepgram"}})
	if got.DeepgramBaseURL != "https://api.deepgram.com" {
		t.Errorf("deepgram_base_url = %q, want https://api.deepgram.com", got.DeepgramBaseURL)
	}
}

func TestNormalizeProviderModelDefaults(t *testing.T) {
	cases := []struct {
		name, provider, translateProvider, wantTranscribe, wantTranslate string
	}{
		{"OpenAI", "openai", "", "gpt-4o-mini-transcribe", "gpt-4o-mini"},
		{"OpenRouter", "openrouter", "", "qwen/qwen3-asr-flash-2026-02-10", "google/gemini-2.5-flash-lite"},
		{"DashScope", "dashscope", "", "qwen3-asr-flash", "qwen-flash"},
		{"Deepgram", "deepgram", "", "nova-2", "gpt-4o-mini"},
		{"Penguin", "penguin", "", "penguin/asr", "penguin/caption-translate"},
		{"explicit translator", "deepgram", "openrouter", "nova-2", "google/gemini-2.5-flash-lite"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeSettings(settingsFile{AudioSettings: domain.AudioSettings{
				APIProvider:       tc.provider,
				TranslateProvider: tc.translateProvider,
			}})
			if got.TranscribeModel != tc.wantTranscribe || got.TranslateModel != tc.wantTranslate {
				t.Fatalf("models = %q/%q, want %q/%q", got.TranscribeModel, got.TranslateModel, tc.wantTranscribe, tc.wantTranslate)
			}
		})
	}
}

func TestNormalizePenguinBaseURL(t *testing.T) {
	original := buildconfig.PenguinBaseURL
	buildconfig.PenguinBaseURL = "https://penguin.example"
	t.Cleanup(func() { buildconfig.PenguinBaseURL = original })

	got := normalizeSettings(settingsFile{AudioSettings: domain.AudioSettings{APIProvider: "penguin"}})
	if got.PenguinBaseURL != "https://penguin.example" {
		t.Errorf("penguin_base_url = %q, want injected build value", got.PenguinBaseURL)
	}
}

func TestApplySettingsPatchAcceptsPenguin(t *testing.T) {
	st, err := ApplySettingsPatch(domain.Settings{}, []byte(`{"api_provider":"penguin","translate_provider":"penguin","transcribe_model":"penguin/asr","translate_model":"penguin/caption-translate"}`))
	if err != nil {
		t.Fatal(err)
	}
	if st.Audio.APIProvider != "penguin" {
		t.Errorf("api_provider = %q, want penguin", st.Audio.APIProvider)
	}
	if st.Audio.TranslateProvider != "penguin" {
		t.Errorf("translate_provider = %q, want penguin", st.Audio.TranslateProvider)
	}
	if st.Audio.TranscribeModel != "penguin/asr" || st.Audio.TranslateModel != "penguin/caption-translate" {
		t.Errorf("models = %q/%q, want penguin/asr and penguin/caption-translate", st.Audio.TranscribeModel, st.Audio.TranslateModel)
	}
}
