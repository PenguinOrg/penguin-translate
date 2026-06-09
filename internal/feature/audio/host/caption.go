package host

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type overlayRubyPair struct {
	JP string `json:"jp"`
	Ro string `json:"ro"`
}

type overlaySegment struct {
	Text     string            `json:"text"`
	English  string            `json:"english"`
	ZhPinyin []overlayRubyPair `json:"zh_pinyin"`
	JpRomaji []overlayRubyPair `json:"jp_romaji"`
	KoRoman  []overlayRubyPair `json:"ko_roman"`
}

func joinReading(pairs []overlayRubyPair) string {
	var parts []string
	for _, p := range pairs {
		if ro := strings.TrimSpace(p.Ro); ro != "" {
			parts = append(parts, ro)
		}
	}
	return strings.Join(parts, " ")
}

func stripLeadingPunctuation(s string) string {
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

func captionLinesFromSegment(seg overlaySegment, wantTranslate bool) (reading, source, english string) {
	source = stripLeadingPunctuation(strings.TrimSpace(seg.Text))
	if source == "" {
		return "", "", ""
	}
	if wantTranslate {
		english = stripLeadingPunctuation(seg.English)
	}
	if pairs := seg.KoRoman; len(pairs) > 0 {
		return joinReading(pairs), source, english
	}
	if pairs := seg.ZhPinyin; len(pairs) > 0 {
		return joinReading(pairs), source, english
	}
	if pairs := seg.JpRomaji; len(pairs) > 0 {
		return joinReading(pairs), source, english
	}
	return "", source, english
}
