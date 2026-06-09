package caption

import (
	"encoding/binary"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var (
	fullZHSentences = []string{
		"请不吝点赞订阅转发打赏支持明镜与点点栏目",
		"大家请不吝点赞订阅转发打赏支持明镜与点点栏目",
		"谢谢大家请不吝点赞订阅转发打赏支持明镜与点点栏目",
		"谢大家请不吝点赞订阅转发打赏支持明镜与点点栏目",
	}
	fullENSentences = []string{
		"thankyouallforyourgenerouslikessubscriptionssharesanddonationstosupportthemirroranddiandianprograms",
		"pleasedonthesitatetolikesubscribeshareandsupportthemirroranddiandianprogramswithdonations",
		"yoyotelevisionseriesexclusive",
		"mingpaocanadamingpaotoronto",
	}
)

const maxExtraChars = 12

func cjkOnly(text string) string {
	var b strings.Builder
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func compactEN(text string) string {
	t := strings.ToLower(norm.NFKD.String(text))
	var b strings.Builder
	for _, r := range t {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	re := regexp.MustCompile(`[^a-z0-9]+`)
	return re.ReplaceAllString(b.String(), "")
}

func matchesFullSentence(haystack, phrase string, minPhraseLen int) bool {
	if phrase == "" || haystack == "" || len(phrase) < minPhraseLen {
		return false
	}
	if !strings.Contains(haystack, phrase) {
		return false
	}
	extra := strings.Replace(haystack, phrase, "", 1)
	return len(extra) <= maxExtraChars
}

var insignificantFillers = map[string]struct{}{
	"嗯": {}, "啊": {}, "呃": {}, "哦": {}, "噢": {}, "唉": {}, "诶": {}, "哈": {},
	"hmm": {}, "hm": {}, "uh": {}, "um": {}, "ah": {}, "eh": {}, "oh": {}, "mhm": {},
}

func ClassifyInsignificantTranscript(text, english string) string {
	zh := strings.TrimSpace(text)
	en := strings.TrimSpace(english)
	if zh == "" && en == "" {
		return "empty"
	}
	zhSig := significantCJKContent(zh)
	enSig := compactEN(en)
	if zhSig == "" && enSig == "" {
		return "punctuation_only"
	}
	if isFillerOnly(zhSig) || isFillerOnly(enSig) {
		return "filler"
	}
	return ""
}

func significantCJKContent(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.IsSpace(r) {
			continue
		}
		if r >= 0x4E00 && r <= 0x9FFF {
			b.WriteRune(r)
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isFillerOnly(sig string) bool {
	if sig == "" {
		return false
	}
	low := strings.ToLower(sig)
	if _, ok := insignificantFillers[low]; ok {
		return true
	}
	for _, r := range sig {
		if !isFillerRune(r) {
			return false
		}
	}
	return true
}

func isFillerRune(r rune) bool {
	switch r {
	case '嗯', '啊', '呃', '哦', '噢', '唉', '诶', '哈':
		return true
	}
	if r <= 127 {
		switch strings.ToLower(string(r)) {
		case "h", "m", "u", "a", "e", "o":
			return true
		}
	}
	_, ok := insignificantFillers[strings.ToLower(string(r))]
	return ok
}

func classifyWhisperArtifact(text, english string) string {
	zh := strings.TrimSpace(text)
	en := strings.TrimSpace(english)
	if zh == "" && en == "" {
		return ""
	}
	cjk := cjkOnly(zh)
	for _, p := range fullZHSentences {
		if matchesFullSentence(cjk, p, 10) {
			return "cta_outro_phrase"
		}
	}
	for _, p := range fullENSentences {
		if matchesFullSentence(compactEN(en), p, 10) || matchesFullSentence(compactEN(zh), p, 10) {
			return "cta_outro_phrase"
		}
	}
	if letterSpacedGarbage(zh) || letterSpacedGarbage(en) {
		return "letter_spaced_garbage"
	}
	return ""
}

func letterSpacedGarbage(text string) bool {
	re := regexp.MustCompile(`[A-Za-z]+`)
	words := re.FindAllString(text, -1)
	if len(words) < 10 {
		return false
	}
	singles := 0
	for _, w := range words {
		if len(w) == 1 {
			singles++
		}
	}
	if float64(singles)/float64(len(words)) < 0.45 {
		return false
	}
	joined := strings.ToLower(strings.Join(words, ""))
	for _, p := range fullENSentences {
		if joined == p {
			return true
		}
	}
	return false
}

func wavPCM16RMS(wav []byte) float64 {
	if len(wav) < 48 {
		return 0
	}
	var data []byte
	off := 12
	for off+8 <= len(wav) {
		cid := string(wav[off : off+4])
		csz := int(binary.LittleEndian.Uint32(wav[off+4:]))
		if cid == "data" {
			end := off + 8 + csz
			if end > len(wav) {
				end = len(wav)
			}
			data = wav[off+8 : end]
			break
		}
		off += 8 + csz
	}
	if len(data) < 4 {
		return 0
	}
	n := len(data) / 2
	var sum float64
	for i := 0; i < n; i++ {
		s := int16(binary.LittleEndian.Uint16(data[i*2:]))
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum/float64(n)) / 32768.0
}

func IsAudioTooQuiet(wav []byte) bool {
	return wavPCM16RMS(wav) < 0.010
}

func dominantScript(text string) string {
	zhRe := regexp.MustCompile(`[\x{4e00}-\x{9fff}]`)
	jaRe := regexp.MustCompile(`[\x{3040}-\x{30ff}]`)
	koRe := regexp.MustCompile(`[\x{ac00}-\x{d7af}]`)
	latinRe := regexp.MustCompile(`[A-Za-z]`)
	scores := []struct {
		tag string
		n   int
	}{
		{"zh", len(zhRe.FindAllString(text, -1))},
		{"ja", len(jaRe.FindAllString(text, -1))},
		{"ko", len(koRe.FindAllString(text, -1))},
		{"latin", len(latinRe.FindAllString(text, -1))},
	}
	best := scores[0]
	for _, s := range scores[1:] {
		if s.n > best.n {
			best = s
		}
	}
	if best.n < 2 {
		return "unknown"
	}
	return best.tag
}

func effectiveLang(hint, text string) string {
	if h := strings.TrimSpace(hint); h != "" {
		return h
	}
	switch d := dominantScript(text); d {
	case "ja", "zh", "ko":
		return d
	default:
		return ""
	}
}

func LanguageHintMismatch(text, hint string) bool {
	hint = strings.ToLower(strings.TrimSpace(hint))
	if len(hint) >= 2 {
		hint = hint[:2]
	}
	if hint != "zh" && hint != "ja" && hint != "ko" {
		return false
	}
	t := strings.TrimSpace(text)
	if utf8.RuneCountInString(t) < 2 {
		return false
	}
	script := dominantScript(t)
	if script == "unknown" {
		return false
	}
	if script == "latin" {
		latin := regexp.MustCompile(`[^A-Za-z]`).ReplaceAllString(t, "")
		return len(latin) >= 8
	}
	return script != hint
}

func StripLeadingPunctuation(text string) string {
	t := strings.TrimSpace(text)
	for t != "" {
		r, size := utf8.DecodeRuneInString(t)
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			t = strings.TrimSpace(t[size:])
			continue
		}
		break
	}
	return t
}
