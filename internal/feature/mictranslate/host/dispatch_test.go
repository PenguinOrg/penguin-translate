package host

import "testing"

// cloudOnlyASR must single out engines the managed Python engine cannot serve.
// Deepgram has no local/Python ASR path, so a Deepgram mic request must always
// take the Go native-cloud route — even when the sidecar is up for an unrelated
// local need (NLLB backtranslate, window translate). Whisper/OpenAI/OpenRouter
// are all serviceable by Python, so they must NOT be forced native-cloud.
func TestCloudOnlyASR(t *testing.T) {
	cloudOnly := []string{"deepgram", "dg", "  Deepgram  ", "DG"}
	for _, e := range cloudOnly {
		if !cloudOnlyASR(e) {
			t.Errorf("cloudOnlyASR(%q) = false, want true", e)
		}
	}

	serviceable := []string{"whisper", "", "openai", "gpt", "openai_whisper", "openrouter", "or", "garbage"}
	for _, e := range serviceable {
		if cloudOnlyASR(e) {
			t.Errorf("cloudOnlyASR(%q) = true, want false", e)
		}
	}
}
