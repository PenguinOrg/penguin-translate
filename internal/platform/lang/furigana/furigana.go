package furigana

import (
	"strings"
	"sync"
	"unicode"

	"github.com/ikawaha/kagome-dict/ipa"
	"github.com/ikawaha/kagome/v2/tokenizer"
	"golang.org/x/text/unicode/norm"
)

type Token struct {
	Surface string `json:"surface"`
	Reading string `json:"reading"`
}

var (
	tokOnce sync.Once
	tok     *tokenizer.Tokenizer
	tokErr  error
)

func tokenizerInstance() (*tokenizer.Tokenizer, error) {
	tokOnce.Do(func() {
		tok, tokErr = tokenizer.New(ipa.Dict())
	})
	return tok, tokErr
}

func hasKanji(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Han) {
			return true
		}
	}
	return false
}

func kataToHira(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x30A1 && r <= 0x30F6 {
			r -= 0x60
		}
		b.WriteRune(r)
	}
	return b.String()
}

func Tokens(japanese string) ([]Token, error) {
	text := strings.TrimSpace(japanese)
	if text == "" {
		return nil, nil
	}
	t, err := tokenizerInstance()
	if err != nil {
		return []Token{{Surface: text, Reading: ""}}, err
	}
	var out []Token
	for _, w := range t.Tokenize(text) {
		if w.Class == tokenizer.DUMMY {
			continue
		}
		surf := w.Surface
		if surf == "" {
			continue
		}
		if surf == "" || isASCII(surf) || !hasKanji(surf) {
			out = append(out, Token{Surface: surf, Reading: ""})
			continue
		}
		reading, ok := w.Reading()
		if !ok || strings.TrimSpace(reading) == "" {
			out = append(out, Token{Surface: surf, Reading: ""})
			continue
		}
		out = append(out, Token{Surface: surf, Reading: kataToHira(norm.NFKC.String(reading))})
	}
	if len(out) == 0 {
		return []Token{{Surface: text, Reading: ""}}, nil
	}
	return out, nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
