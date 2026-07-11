package host

import (
	"testing"

	"translation-overlay/internal/platform/buildconfig"
	"translation-overlay/internal/platform/domain"
)

func TestNormalizersAcceptPenguin(t *testing.T) {
	if got := normalizeASREngine("penguin"); got != "penguin" {
		t.Errorf("normalizeASREngine(penguin) = %q", got)
	}
	if got := normalizeAPIProvider("penguin"); got != "penguin" {
		t.Errorf("normalizeAPIProvider(penguin) = %q", got)
	}
	if got := normalizeAssessmentMode("penguin"); got != "penguin" {
		t.Errorf("normalizeAssessmentMode(penguin) = %q", got)
	}
	if got := normalizeAssessmentMode("azure"); got != "azure" {
		t.Errorf("normalizeAssessmentMode(azure) = %q", got)
	}
	if got := normalizeAssessmentMode("bogus"); got != "basic" {
		t.Errorf("normalizeAssessmentMode(bogus) = %q", got)
	}
}

func TestNormalizeSettingsPenguinDefaults(t *testing.T) {
	original := buildconfig.PenguinBaseURL
	buildconfig.PenguinBaseURL = "https://penguin.example"
	t.Cleanup(func() { buildconfig.PenguinBaseURL = original })

	got := normalizeSettings(settingsFile{MicTranslateSettings: domain.MicTranslateSettings{
		EnglishASREngine: "penguin",
	}})
	if got.EnglishASREngine != "penguin" {
		t.Errorf("english_asr_engine = %q, want penguin", got.EnglishASREngine)
	}
	if got.TranscribeModel != "penguin/asr" {
		t.Errorf("transcribe_model = %q, want penguin/asr", got.TranscribeModel)
	}
	if got.PenguinBaseURL != "https://penguin.example" {
		t.Errorf("penguin_base_url = %q, want default", got.PenguinBaseURL)
	}
}

func TestNormalizeSettingsPenguinTranslateModel(t *testing.T) {
	got := normalizeSettings(settingsFile{MicTranslateSettings: domain.MicTranslateSettings{
		ForwardTranslator: "openai",
		APIProvider:       "penguin",
	}})
	if got.APIProvider != "penguin" {
		t.Errorf("api_provider = %q, want penguin", got.APIProvider)
	}
	if got.TranslateModel != "penguin/translate" {
		t.Errorf("translate_model = %q, want penguin/translate", got.TranslateModel)
	}
}

func TestApplySettingsPatchAcceptsPenguin(t *testing.T) {
	st, err := ApplySettingsPatch(domain.Settings{PenguinAPIKey: "pgn_k"}, []byte(`{"english_asr_engine":"penguin","assessment_mode":"penguin"}`))
	if err != nil {
		t.Fatal(err)
	}
	if st.MicTranslate.EnglishASREngine != "penguin" {
		t.Errorf("english_asr_engine = %q, want penguin", st.MicTranslate.EnglishASREngine)
	}
	if st.MicTranslate.JaRepeatASREngine != "penguin" {
		t.Errorf("ja_repeat_asr_engine = %q, want penguin", st.MicTranslate.JaRepeatASREngine)
	}
	if st.MicTranslate.AssessmentMode != "penguin" {
		t.Errorf("assessment_mode = %q, want penguin", st.MicTranslate.AssessmentMode)
	}
	if st.PenguinAPIKey != "pgn_k" {
		t.Errorf("patch dropped the penguin key")
	}
}
