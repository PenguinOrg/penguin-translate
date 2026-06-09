package caption

import (
	"strings"
	"unicode/utf8"

	"github.com/mozillazg/go-pinyin"

	"translation-overlay/internal/platform/lang/furigana"
)

type RubyPair struct {
	JP string `json:"jp"`
	Ro string `json:"ro"`
}

type Enrichment struct {
	Japanese string     `json:"japanese,omitempty"`
	Romaji   string     `json:"romaji,omitempty"`
	JPRomaji []RubyPair `json:"jp_romaji,omitempty"`
	ZhPinyin []RubyPair `json:"zh_pinyin,omitempty"`
	KoRoman  []RubyPair `json:"ko_roman,omitempty"`
}

func textMostlyHangul(text string) bool {
	hangul, other := 0, 0
	for _, r := range text {
		if r >= 0xAC00 && r <= 0xD7AF {
			hangul++
		} else if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			other++
		}
	}
	return hangul > 0 && hangul >= other
}

func EnrichLine(text, lang string) Enrichment {
	t := strings.TrimSpace(text)
	if t == "" {
		return Enrichment{}
	}
	if textMostlyHangul(t) {
		return enrichKorean(t)
	}
	lg := strings.ToLower(strings.TrimSpace(lang))
	if len(lg) >= 2 {
		lg = lg[:2]
	}
	switch lg {
	case "ko":
		return enrichKorean(t)
	case "ja":
		return enrichJapanese(t)
	case "zh":
		return enrichChinese(t)
	default:
		return Enrichment{Japanese: t}
	}
}

func enrichJapanese(text string) Enrichment {
	tokens, err := furigana.Tokens(text)
	if err != nil || len(tokens) == 0 {
		return Enrichment{Japanese: text}
	}
	var pairs []RubyPair
	var readings []string
	for _, tok := range tokens {
		ro := hiraganaToRomaji(tok.Reading)
		pairs = append(pairs, RubyPair{JP: tok.Surface, Ro: ro})
		if ro != "" {
			readings = append(readings, ro)
		}
	}
	romaji := strings.Join(readings, " ")
	if romaji == "" {
		romaji = hiraganaToRomaji(text)
	}
	return Enrichment{
		Japanese: text,
		Romaji:   romaji,
		JPRomaji: pairs,
	}
}

func enrichChinese(text string) Enrichment {
	args := pinyin.NewArgs()
	args.Style = pinyin.Tone
	py := pinyin.Pinyin(text, args)
	var pairs []RubyPair
	for i, r := range text {
		ro := ""
		if i < len(py) && len(py[i]) > 0 {
			ro = py[i][0]
		}
		pairs = append(pairs, RubyPair{JP: string(r), Ro: ro})
	}
	return Enrichment{ZhPinyin: pairs}
}

func enrichKorean(text string) Enrichment {
	var pairs []RubyPair
	for _, r := range text {
		ch := string(r)
		if r >= 0xAC00 && r <= 0xD7AF {
			pairs = append(pairs, RubyPair{JP: ch, Ro: hangulSyllableRomanize(r)})
		} else if r == ' ' || r == '\u00a0' {
			pairs = append(pairs, RubyPair{JP: "\u00a0", Ro: ""})
		} else {
			pairs = append(pairs, RubyPair{JP: ch, Ro: ""})
		}
	}
	return Enrichment{KoRoman: pairs}
}

func hangulSyllableRomanize(r rune) string {
	if r < 0xAC00 || r > 0xD7AF {
		return ""
	}
	s := r - 0xAC00
	initials := []string{
		"g", "gg", "n", "d", "dd", "r", "m", "b", "bb", "s", "ss", "", "j", "jj", "ch", "k", "t", "p", "h",
	}
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

func hiraganaToRomaji(s string) string {
	if s == "" {
		return ""
	}
	var out strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == 'っ' && i+1 < len(runes) {
			nxt := kanaToRomaji(runes[i+1], "")
			if nxt != "" {
				out.WriteByte(nxt[0])
			}
			continue
		}
		next := ""
		if i+1 < len(runes) {
			next = string(runes[i+1])
		}
		out.WriteString(kanaToRomaji(r, next))
		if next != "" && isSmallKana(runes[i+1]) {
			i++
		}
	}
	return strings.TrimSpace(out.String())
}

func isSmallKana(r rune) bool {
	return strings.ContainsRune("ゃゅょぁぃぅぇぉャュョァィゥェォ", r)
}

func kanaToRomaji(r rune, next string) string {
	table := map[rune]string{
		'あ': "a", 'い': "i", 'う': "u", 'え': "e", 'お': "o",
		'か': "ka", 'き': "ki", 'く': "ku", 'け': "ke", 'こ': "ko",
		'さ': "sa", 'し': "shi", 'す': "su", 'せ': "se", 'そ': "so",
		'た': "ta", 'ち': "chi", 'つ': "tsu", 'て': "te", 'と': "to",
		'な': "na", 'に': "ni", 'ぬ': "nu", 'ね': "ne", 'の': "no",
		'は': "ha", 'ひ': "hi", 'ふ': "fu", 'へ': "he", 'ほ': "ho",
		'ま': "ma", 'み': "mi", 'む': "mu", 'め': "me", 'も': "mo",
		'や': "ya", 'ゆ': "yu", 'よ': "yo",
		'ら': "ra", 'り': "ri", 'る': "ru", 'れ': "re", 'ろ': "ro",
		'わ': "wa", 'を': "wo", 'ん': "n",
		'が': "ga", 'ぎ': "gi", 'ぐ': "gu", 'げ': "ge", 'ご': "go",
		'ざ': "za", 'じ': "ji", 'ず': "zu", 'ぜ': "ze", 'ぞ': "zo",
		'だ': "da", 'ぢ': "ji", 'づ': "zu", 'で': "de", 'ど': "do",
		'ば': "ba", 'び': "bi", 'ぶ': "bu", 'べ': "be", 'ぼ': "bo",
		'ぱ': "pa", 'ぴ': "pi", 'ぷ': "pu", 'ぺ': "pe", 'ぽ': "po",
		'ー': "-",
	}
	if r >= 0x30A1 && r <= 0x30F6 {
		r -= 0x60
	}
	if v, ok := table[r]; ok {
		if next != "" {
			if nr, ok := table[[]rune(next)[0]]; ok && len(nr) > 1 && strings.HasPrefix(nr, "y") {
				return v[:len(v)-1] + nr
			}
		}
		return v
	}
	if r < 128 {
		return string(r)
	}
	if utf8.RuneLen(r) > 0 {
		return string(r)
	}
	return ""
}
