package translate

import (
	"net/url"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

var (
	skipWordsMu sync.RWMutex
	skipWords   []string
)

func SetSkipWords(words []string) {
	skipWordsMu.Lock()
	skipWords = normalizeSkipWords(words)
	skipWordsMu.Unlock()
}

func normalizeSkipWords(words []string) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		key := strings.ToLower(w)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, w)
	}
	return out
}

func matchesUserSkip(line string) bool {
	skipWordsMu.RLock()
	terms := skipWords
	skipWordsMu.RUnlock()
	if len(terms) == 0 {
		return false
	}
	trim := strings.TrimSpace(line)
	low := strings.ToLower(trim)
	for _, term := range terms {
		t := strings.TrimSpace(term)
		if t == "" {
			continue
		}
		if strings.EqualFold(trim, t) {
			return true
		}
		if strings.Contains(low, strings.ToLower(t)) {
			return true
		}
	}
	return false
}

func ShouldSkipTranslation(line string) bool {
	return matchesUserSkip(line)
}

func ShouldSkipLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return true
	}
	if looksLikeURL(line) {
		return true
	}
	if strings.Contains(line, "http") && len(line) < 120 {
		return true
	}
	low := strings.ToLower(line)
	noise := []string{
		"ps c:\\", "powershell", ".ps1", ".py", ".json",
		"window-translate", "oneocr", "appdata\\roaming",
		"overlay enabled", "gui at http", "go build",
		"cursor", "composer",
	}
	for _, n := range noise {
		if strings.Contains(low, n) {
			return true
		}
	}

	if len(line) > 30 && !HasCJK(line) && strings.ContainsAny(line, "\\/") {
		return true
	}
	return false
}

func looksLikeURL(s string) bool {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		_, err := url.Parse(s)
		return err == nil || strings.Contains(s, ".")
	}
	return false
}

func StripLeadingPunctuation(s string) string {
	s = strings.TrimSpace(s)
	for s != "" {
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError {
			break
		}
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			s = strings.TrimSpace(s[size:])
			continue
		}
		break
	}
	return s
}

func SanitizeForOverlay(en string) string {
	en = StripLeadingPunctuation(en)
	if en == "" {
		return ""
	}
	if IsRefusal(en) {
		return ""
	}
	if len(en) > 240 {
		en = en[:237] + "..."
	}
	return en
}

func IsRefusal(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	if len(low) < 40 {
		return false
	}
	phrases := []string{
		"cannot access external",
		"i'm sorry, but i cannot",
		"i am sorry, but i cannot",
		"however, if you provide",
		"as an ai language model",
	}
	for _, p := range phrases {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

func HasCJK(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Hiragana, unicode.Katakana) {
			return true
		}
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}
