package host

import (
	"testing"

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
