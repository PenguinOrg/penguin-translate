package host

import "testing"

func TestCloudOnlyASR(t *testing.T) {
	cloudOnly := []string{"deepgram", "dg", "  Deepgram  ", "DG", "dashscope", "ds", "  DashScope  ", "penguin"}
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
