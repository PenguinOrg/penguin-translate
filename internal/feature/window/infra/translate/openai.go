package translate

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"translation-overlay/internal/feature/mictranslate/infra/languages"
	"translation-overlay/internal/platform/cloudapi"
)

type Client struct {
	Creds  cloudapi.Credentials
	Model  string
	Target string
}

func New(creds cloudapi.Credentials, model string) *Client {
	return &Client{Creds: creds, Model: model}
}

func (c *Client) ToTargetLine(text, sourceLang string) (LineResult, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return LineResult{}, nil
	}
	sys := lineSystemPrompt(sourceLangName(detectLang(text, sourceLang)), c.Target)
	content, err := cloudapi.ChatCompletionWith(c.Creds, c.Model, sys, text, cloudapi.ChatOptions{
		Temperature:    0.2,
		ResponseFormat: "json_object",
		MaxTokens:      2000,
	})
	if err != nil {
		return LineResult{}, err
	}
	content = stripCodeFence(strings.TrimSpace(content))
	var payload struct {
		En     string `json:"en"`
		Roman  string `json:"roman"`
		Pinyin string `json:"pinyin"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return LineResult{}, fmt.Errorf("parse line JSON: %w (got %s)", err, truncate(content, 200))
	}
	en := StripLeadingPunctuation(strings.TrimSpace(payload.En))
	if IsRefusal(en) {
		en = text
	}
	roman := strings.TrimSpace(payload.Roman)
	if roman == "" {
		roman = strings.TrimSpace(payload.Pinyin)
	}
	return LineResult{En: en, Roman: roman}, nil
}

func detectLang(text, hint string) string {
	h := strings.ToLower(strings.TrimSpace(hint))
	if h == "ja" || h == "zh" || h == "en" {
		return h
	}
	var jp, zh, other int
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			jp++
		case r >= 0x4e00 && r <= 0x9fff:
			zh++
		default:
			if !unicode.IsSpace(r) {
				other++
			}
		}
	}
	if jp > zh && jp > 0 {
		return "ja"
	}
	if zh > jp && zh > 0 {
		return "zh"
	}
	if zh > 0 || jp > 0 {
		if jp >= zh {
			return "ja"
		}
		return "zh"
	}
	return "ja"
}

func sourceLangName(lang string) string {
	if name := map[string]string{"ja": "Japanese", "zh": "Chinese", "en": "English"}[lang]; name != "" {
		return name
	}
	return lang
}

func targetLangName(id string) (name string, romanAid bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" || id == "en" {
		return "English", true
	}
	if l, ok := languages.Lang(id); ok {
		return l.Label, false
	}
	return id, false
}

func lineSystemPrompt(srcName, target string) string {
	tgtName, romanAid := targetLangName(target)
	if romanAid {
		return fmt.Sprintf(
			"Translate %s UI text to concise English. Return ONLY valid JSON: {\"en\":\"...\",\"roman\":\"...\"}. "+
				"en = English translation. roman = compact romanization of the SOURCE — pinyin with tone marks for Chinese, modified Hepburn romaji for Japanese; empty if source is Latin/numbers only. "+
				"No markdown. URLs and mostly Latin/numbers: unchanged en, empty roman.",
			srcName,
		)
	}
	return fmt.Sprintf(
		"Translate %s UI text to concise %s. Return ONLY valid JSON: {\"en\":\"...\",\"roman\":\"\"}. "+
			"en = the %s translation. roman = empty string. "+
			"No markdown. Leave URLs, numbers and text already in %s unchanged in en.",
		srcName, tgtName, tgtName, tgtName,
	)
}

func batchSystemPrompt(srcName, target string, n int) string {
	tgtName, romanAid := targetLangName(target)
	if romanAid {
		return fmt.Sprintf(
			"Translate %s UI text to concise English. Input is JSON array of {id,text}. "+
				"Return ONLY valid JSON: {\"items\":[{\"id\":0,\"en\":\"...\",\"roman\":\"...\"}, ...]} with one object per input id (0..%d). "+
				"Copy each id exactly. No markdown. "+
				"For each line: en = English translation; roman = compact romanization of the SOURCE text — pinyin with tone marks for Chinese, modified Hepburn romaji for Japanese; empty string if the source is already Latin/numbers only. "+
				"URLs and mostly Latin/numbers: return unchanged in en, roman empty.",
			srcName, n-1,
		)
	}
	return fmt.Sprintf(
		"Translate %s UI text to concise %s. Input is JSON array of {id,text}. "+
			"Return ONLY valid JSON: {\"items\":[{\"id\":0,\"en\":\"...\",\"roman\":\"\"}, ...]} with one object per input id (0..%d). "+
			"Copy each id exactly. No markdown. "+
			"For each line: en = the %s translation; roman = empty string. "+
			"Leave URLs, numbers and text already in %s unchanged in en.",
		srcName, tgtName, n-1, tgtName, tgtName,
	)
}

var fenceRE = regexp.MustCompile("(?s)^```(?:\\w+)?\\n?(.*)\\n?```$")

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if m := fenceRE.FindStringSubmatch(s); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
