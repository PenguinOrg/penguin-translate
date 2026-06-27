package caption

import "strings"

const vrGameVocab = "avatar, avatars, instance, world, lobby, portal, mirror, fullbody tracking, FBT, IK, OSC, prefab, Quest, PCVR, desktop user, shader, Udon, SDK, nameplate, trust rank, friends plus, invite plus, group instance, crasher, gesture, emote"

var langNames = map[string]string{
	"ja": "Japanese", "zh": "Chinese", "ko": "Korean", "en": "English",
	"es": "Spanish", "fr": "French", "de": "German", "ru": "Russian",
	"pt": "Portuguese", "it": "Italian", "id": "Indonesian", "th": "Thai",
	"vi": "Vietnamese", "ar": "Arabic", "hi": "Hindi",
}

func languageName(code string) string {
	c := strings.ToLower(strings.TrimSpace(code))
	if len(c) >= 2 {
		c = c[:2]
	}
	if n, ok := langNames[c]; ok {
		return n
	}
	return strings.ToUpper(c)
}

func BuildCaptionContext(enabled bool, langs []string, customHint string) string {
	if !enabled {
		return ""
	}
	var b strings.Builder
	b.WriteString("This is social VR voice chat audio. Transcribe accurately and preserve proper nouns and gaming/VR jargon. ")
	if names := languageList(langs); names != "" {
		b.WriteString("Expected spoken languages: ")
		b.WriteString(names)
		b.WriteString(". ")
	}
	b.WriteString("Common VR/gaming vocabulary: ")
	b.WriteString(vrGameVocab)
	b.WriteString(".")
	if h := strings.TrimSpace(customHint); h != "" {
		b.WriteString(" Names and terms likely in this session: ")
		b.WriteString(h)
		b.WriteString(".")
	}
	return b.String()
}

func BuildTranslateContext(enabled bool, customHint string) string {
	if !enabled {
		return ""
	}
	var b strings.Builder
	b.WriteString("Keep proper nouns and VR/gaming terms intact and consistent; do not translate names. Common VR/gaming vocabulary: ")
	b.WriteString(vrGameVocab)
	b.WriteString(".")
	if h := strings.TrimSpace(customHint); h != "" {
		b.WriteString(" Names and terms in this session: ")
		b.WriteString(h)
		b.WriteString(".")
	}
	return b.String()
}

func languageList(langs []string) string {
	seen := map[string]bool{}
	out := make([]string, 0, len(langs))
	for _, l := range langs {
		name := languageName(l)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return strings.Join(out, ", ")
}
