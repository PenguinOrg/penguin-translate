package vrchatosc

import (
	"testing"

	"translation-overlay/internal/feature/mictranslate/infra/plugin"
)

func TestFormatChatboxText(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Hello.", "Hello"},
		{"こんにちは。", "こんにちは"},
		{"Hi there.  ", "Hi there"},
		{"Still going...", "Still going"},
		{"Question?", "Question?"},
		{"Wow!", "Wow!"},
		{"  spaced   words  ", "spaced words"},
		{"", ""},
	}
	for _, tc := range tests {
		got := formatChatboxText(tc.in)
		if got != tc.want {
			t.Errorf("formatChatboxText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatChatboxTextDoesNotStripAllDots(t *testing.T) {
	got := formatChatboxText("v1.2 release.")
	if got != "v1.2 release" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatChatboxTextMultiline(t *testing.T) {
	got := formatChatboxText("Hello.\nこんにちは。")
	want := "Hello\nこんにちは"
	if got != want {
		t.Fatalf("formatChatboxText multiline = %q, want %q", got, want)
	}
}

func TestComposeChatboxText(t *testing.T) {
	if got := composeChatboxText(true, "Hi.", "やあ。"); got != "Hi.\nやあ。" {
		t.Fatalf("include english: %q", got)
	}
	if got := composeChatboxText(false, "Hi.", "やあ。"); got != "やあ。" {
		t.Fatalf("target only: %q", got)
	}
}

func TestComposeConversationChatbox(t *testing.T) {
	c := &plugin.ConversationPayload{
		SourceLang: "en",
		SourceText: "Hello there",
		Lines: []plugin.ConversationLine{
			{Lang: "ja", Text: "やあ"},
			{Lang: "ko", Text: "안녕"},
			{Lang: "zh", Text: ""},
		},
	}
	if got := composeConversationChatbox(false, c); got != "やあ\n안녕" {
		t.Fatalf("translations only: %q", got)
	}
	if got := composeConversationChatbox(true, c); got != "Hello there\nやあ\n안녕" {
		t.Fatalf("with original: %q", got)
	}
}
