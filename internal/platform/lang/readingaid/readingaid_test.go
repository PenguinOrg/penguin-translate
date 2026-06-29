package readingaid

import "testing"

func TestPinyinTokens(t *testing.T) {
	toks := Tokens("pinyin", "你好，世界")
	// Han chars get a reading; punctuation is grouped with no reading.
	var han, withReading int
	for _, tk := range toks {
		if r := []rune(tk.Surface); len(r) == 1 && r[0] >= 0x4E00 && r[0] <= 0x9FFF {
			han++
			if tk.Reading != "" {
				withReading++
			}
		}
	}
	if han != 4 {
		t.Fatalf("want 4 Han tokens, got %d (%+v)", han, toks)
	}
	if withReading != 4 {
		t.Errorf("want every Han token to carry pinyin, got %d/%d (%+v)", withReading, han, toks)
	}
}

func TestRomajaTokens(t *testing.T) {
	toks := Tokens("romaja", "안녕하세요")
	if len(toks) != 5 {
		t.Fatalf("want 5 syllable tokens, got %d (%+v)", len(toks), toks)
	}
	for _, tk := range toks {
		if tk.Reading == "" {
			t.Errorf("syllable %q missing romaja", tk.Surface)
		}
	}
	if toks[0].Reading != "an" {
		t.Errorf("want 안 -> \"an\", got %q", toks[0].Reading)
	}
}

func TestNoneYieldsNil(t *testing.T) {
	if got := Tokens("none", "hello"); got != nil {
		t.Errorf("want nil for unsupported aid, got %+v", got)
	}
}
