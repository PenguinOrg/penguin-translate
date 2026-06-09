package caption

import "testing"

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
