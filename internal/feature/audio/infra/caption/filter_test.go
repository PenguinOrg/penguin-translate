package caption

import "testing"

func TestLanguageHintMismatch(t *testing.T) {
	cases := []struct {
		text, hint string
		want       bool
	}{
		{"Understood. Please provide the audio you would like transcribed.", "ja", true},
		{"I'm sorry, but I can't process audio or speech samples.", "zh", true},
		{"こんにちは、元気ですか", "ja", false},
		{"你好，今天怎么样", "zh", false},
		{"Hello, how are you", "en", false},
		{"OK", "ja", false},
		{"Avatar", "ja", false},
		{"Understood. Please provide the audio.", "", false},
	}
	for _, tc := range cases {
		if got := LanguageHintMismatch(tc.text, tc.hint); got != tc.want {
			t.Errorf("LanguageHintMismatch(%q, %q) = %v, want %v", tc.text, tc.hint, got, tc.want)
		}
	}
}

func TestClassifyInsignificantTranscript(t *testing.T) {
	cases := []struct {
		zh, en, want string
	}{
		{"", "", "empty"},
		{"。", "", "punctuation_only"},
		{"…", "", "punctuation_only"},
		{"嗯", "", "filler"},
		{"嗯。", "Hmm.", "filler"},
		{"啊", "", "filler"},
		{"Hmm.", "", "filler"},
		{"排在第四名。", "Ranked fourth.", ""},
		{"好的", "Okay.", ""},
		{"", "Okay.", ""},
	}
	for _, tc := range cases {
		got := ClassifyInsignificantTranscript(tc.zh, tc.en)
		if got != tc.want {
			t.Errorf("ClassifyInsignificantTranscript(%q, %q) = %q, want %q", tc.zh, tc.en, got, tc.want)
		}
	}
}
