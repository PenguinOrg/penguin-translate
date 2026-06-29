// Package readingaid produces per-word/character reading-aid tokens (furigana
// for Japanese, pinyin for Chinese, romaja for Korean) for the translate-text
// path, so the conversation surface and the practice panel render the same ruby
// the caption pipeline already shows.
package readingaid

import (
	"strings"
	"unicode"

	"github.com/mozillazg/go-pinyin"

	"translation-overlay/internal/platform/lang/furigana"
)

type Token struct {
	Surface string `json:"surface"`
	Reading string `json:"reading"`
}

// Tokens returns reading-aid tokens for aid ("furigana" | "pinyin" | "romaja").
// Any other aid (e.g. "none") yields nil.
func Tokens(aid, text string) []Token {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	switch aid {
	case "furigana":
		toks, err := furigana.Tokens(text)
		if err != nil {
			return nil
		}
		out := make([]Token, 0, len(toks))
		for _, t := range toks {
			out = append(out, Token{Surface: t.Surface, Reading: t.Reading})
		}
		return out
	case "pinyin":
		return pinyinTokens(text)
	case "romaja":
		return romajaTokens(text)
	}
	return nil
}

// AsMaps is the wire shape used by the translate-text JSON rows.
func AsMaps(toks []Token) []map[string]string {
	out := make([]map[string]string, 0, len(toks))
	for _, t := range toks {
		out = append(out, map[string]string{"surface": t.Surface, "reading": t.Reading})
	}
	return out
}

func pinyinTokens(text string) []Token {
	args := pinyin.NewArgs()
	args.Style = pinyin.Tone
	py := pinyin.Pinyin(text, args)
	var out []Token
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, Token{Surface: buf.String()})
			buf.Reset()
		}
	}
	pi := 0
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			flush()
			ro := ""
			if pi < len(py) && len(py[pi]) > 0 {
				ro = py[pi][0]
			}
			pi++
			out = append(out, Token{Surface: string(r), Reading: ro})
		} else {
			buf.WriteRune(r)
		}
	}
	flush()
	return out
}

func romajaTokens(text string) []Token {
	var out []Token
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, Token{Surface: buf.String()})
			buf.Reset()
		}
	}
	for _, r := range text {
		if r >= 0xAC00 && r <= 0xD7AF {
			flush()
			out = append(out, Token{Surface: string(r), Reading: hangulRomanize(r)})
		} else {
			buf.WriteRune(r)
		}
	}
	flush()
	return out
}

// hangulRomanize maps a precomposed Hangul syllable to Revised Romanization
// (same decomposition the caption enrichment uses).
func hangulRomanize(r rune) string {
	if r < 0xAC00 || r > 0xD7AF {
		return ""
	}
	s := r - 0xAC00
	initials := []string{"g", "gg", "n", "d", "dd", "r", "m", "b", "bb", "s", "ss", "", "j", "jj", "ch", "k", "t", "p", "h"}
	medials := []string{"a", "ae", "ya", "yae", "eo", "e", "yeo", "ye", "o", "wa", "wae", "oe", "yo", "u", "wo", "we", "wi", "yu", "eu", "ui", "i"}
	finals := []string{"", "g", "gg", "gs", "n", "nj", "nh", "d", "l", "lg", "lm", "lb", "ls", "lt", "lp", "lh", "m", "b", "bs", "s", "ss", "ng", "j", "ch", "k", "t", "p", "h"}
	i := s / (21 * 28)
	m := (s / 28) % 21
	f := s % 28
	if int(i) >= len(initials) || int(m) >= len(medials) || int(f) >= len(finals) {
		return ""
	}
	return initials[i] + medials[m] + finals[f]
}
