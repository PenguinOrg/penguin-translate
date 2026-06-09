package furigana

import (
	"strings"
	"testing"
)

func TestTokensBlankInput(t *testing.T) {
	got, err := Tokens("   ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("blank input should yield nil tokens, got %v", got)
	}
}

func TestTokensASCIIHasNoReading(t *testing.T) {
	got, err := Tokens("hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, tk := range got {
		if tk.Reading != "" {
			t.Errorf("ASCII token %q got reading %q, want empty", tk.Surface, tk.Reading)
		}
	}
}

func TestTokensDropBoundaryMarkers(t *testing.T) {
	got, err := Tokens("あなた")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected tokens for あなた")
	}
	for _, tk := range got {
		if tk.Surface == "BOS" || tk.Surface == "EOS" {
			t.Errorf("tokens leaked kagome boundary marker %q (renders as BOS…EOS in the UI)", tk.Surface)
		}
	}
}

func TestTokensKanjiReadingIsHiragana(t *testing.T) {
	const in = "日本語"
	got, err := Tokens(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var surfaces string
	sawReading := false
	for _, tk := range got {
		surfaces += tk.Surface
		if tk.Reading == "" {
			continue
		}
		sawReading = true
		for _, r := range tk.Reading {
			if r >= 0x30A1 && r <= 0x30F6 {
				t.Errorf("reading %q for %q contains katakana", tk.Reading, tk.Surface)
			}
		}
	}
	if !strings.Contains(surfaces, in) {
		t.Errorf("token surfaces %q do not contain input %q", surfaces, in)
	}
	if !sawReading {
		t.Errorf("expected at least one kanji token to carry a reading for %q", in)
	}
}
