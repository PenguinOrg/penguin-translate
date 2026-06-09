package nativecloud

import (
	"fmt"
	"strings"

	"translation-overlay/internal/feature/mictranslate/infra/languages"
	"translation-overlay/internal/platform/cloudapi"
	"translation-overlay/internal/platform/lang/furigana"
)

type Settings struct {
	TargetLanguage            string
	ForwardTranslator         string
	EnglishASREngine          string
	JaRepeatASREngine         string
	Backtranslate             string
	PracticeEnabled           bool
	APIProvider               string
	TranscribeModel           string
	TranslateModel            string
	OpenAIForwardModel        string
	OpenAIBacktransModel      string
	OpenAITranscribeModel     string
	OpenAIWhisperModel        string
	OpenRouterTranscribeModel string
	OpenAIKey                 string
	OpenAIBase                string
	OpenRouterKey             string
	OpenRouterBase            string
}

func (s Settings) ToCredentials() cloudapi.Credentials {
	return cloudapi.Credentials{
		OpenAIKey:      s.OpenAIKey,
		OpenAIBase:     s.OpenAIBase,
		OpenRouterKey:  s.OpenRouterKey,
		OpenRouterBase: s.OpenRouterBase,
		APIProvider:    s.APIProvider,
	}
}

func (s Settings) targetKind() string {
	id := strings.ToLower(strings.TrimSpace(s.TargetLanguage))
	switch id {
	case "zh", "cn", "chinese":
		return "zh"
	case "ko", "kr", "korean":
		return "ko"
	default:
		return "jp"
	}
}

func (s Settings) forwardModel() string {
	if m := strings.TrimSpace(s.TranslateModel); m != "" {
		return m
	}
	return strings.TrimSpace(s.OpenAIForwardModel)
}

func (s Settings) backModel() string {
	return strings.TrimSpace(s.OpenAIBacktransModel)
}

func (s Settings) transcribeModel(asrEngine string) string {
	if m := strings.TrimSpace(s.TranscribeModel); m != "" {
		return m
	}
	switch cloudapi.NormalizeASREngine(asrEngine) {
	case "openrouter":
		return strings.TrimSpace(s.OpenRouterTranscribeModel)
	default:
		if strings.TrimSpace(s.OpenAITranscribeModel) != "" {
			return strings.TrimSpace(s.OpenAITranscribeModel)
		}
		return strings.TrimSpace(s.OpenAIWhisperModel)
	}
}

func effectiveBacktranslate(s Settings) string {
	if !s.PracticeEnabled {
		return "none"
	}
	bt := strings.ToLower(strings.TrimSpace(s.Backtranslate))
	if bt != "none" && bt != "local" && bt != "openai" {
		return "local"
	}
	return bt
}

type TranslateResult map[string]any

func TranslateEnglish(s Settings, english string) (TranslateResult, error) {
	english = strings.TrimSpace(english)
	kind := s.targetKind()
	prof := languages.Get(s.TargetLanguage)
	target := ""
	var err error
	if english != "" {
		if strings.ToLower(strings.TrimSpace(s.ForwardTranslator)) == "openai" {
			target, err = cloudapi.TranslateEnglishToTarget(s.ToCredentials(), s.forwardModel(), kind, english)
		} else {
			return nil, fmt.Errorf("local NLLB forward translation requires the Python engine")
		}
		if err != nil {
			return nil, err
		}
	}
	furi := []map[string]string{}
	if target != "" && kind == "jp" {
		toks, _ := furigana.Tokens(target)
		for _, t := range toks {
			furi = append(furi, map[string]string{"surface": t.Surface, "reading": t.Reading})
		}
	}
	back := ""
	bt := effectiveBacktranslate(s)
	if target != "" && bt == "openai" {
		back, err = cloudapi.BacktranslateTargetToEnglish(s.ToCredentials(), s.backModel(), kind, target)
		if err != nil {
			back = ""
		}
	}
	return TranslateResult{
		"english":            english,
		"japanese":           target,
		"target":             target,
		"target_lang":        prof.ID,
		"furigana":           furi,
		"back_english":       back,
		"detected_language":  "en",
		"english_asr_engine": "manual",
		"english_asr_model":  "typed",
	}, nil
}

func readingAidTokens(aid languages.ReadingAid, text string) []map[string]string {
	out := []map[string]string{}
	if text == "" {
		return out
	}
	switch aid {
	case languages.ReadingAidFurigana:
		toks, _ := furigana.Tokens(text)
		for _, t := range toks {
			out = append(out, map[string]string{"surface": t.Surface, "reading": t.Reading})
		}
	}
	return out
}

func TranslateOne(s Settings, srcID, tgtID, text string) (map[string]any, error) {
	text = strings.TrimSpace(text)
	tgt := languages.LangOr(tgtID)
	srcLabel := "the source language"
	if src, ok := languages.Lang(srcID); ok {
		srcLabel = src.Label
	}
	out := map[string]any{
		"language":           tgt.ID,
		"label":              tgt.Label,
		"flag":               tgt.Flag,
		"text":               "",
		"reading_aid":        string(tgt.ReadingAid),
		"reading_aid_tokens": []map[string]string{},
	}
	if text == "" {
		return out, nil
	}
	if strings.ToLower(strings.TrimSpace(s.ForwardTranslator)) != "openai" {
		return nil, fmt.Errorf("local NLLB translation requires the Python engine")
	}
	translated, err := cloudapi.Translate(s.ToCredentials(), s.forwardModel(), srcLabel, tgt.Label, text)
	if err != nil {
		return nil, err
	}
	out["text"] = translated
	out["reading_aid_tokens"] = readingAidTokens(tgt.ReadingAid, translated)
	return out, nil
}

func TranslateMulti(s Settings, srcID string, tgtIDs []string, text string) ([]map[string]any, error) {
	results := []map[string]any{}
	srcCanon := languages.CanonicalID(srcID)
	seen := map[string]bool{}
	for _, tgtID := range tgtIDs {
		canon := languages.CanonicalID(tgtID)
		if canon == "" || canon == srcCanon || seen[canon] {
			continue
		}
		seen[canon] = true
		row, err := TranslateOne(s, srcID, tgtID, text)
		if err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	return results, nil
}

func PipelineFromWAV(s Settings, wav []byte, speechLang string) (TranslateResult, error) {
	asr := s.EnglishASREngine
	text, det, err := cloudapi.CloudTranscribe(s.ToCredentials(), asr, s.transcribeModel(asr), speechLang, wav)
	if err != nil {
		return nil, err
	}
	out, err := TranslateEnglish(s, text)
	if err != nil {
		return nil, err
	}
	out["pipeline"] = "split"
	out["english_asr_engine"] = asr
	out["english_asr_model"] = s.transcribeModel(asr)
	if det != nil {
		out["detected_language"] = *det
	} else if speechLang != "" {
		out["detected_language"] = speechLang
	}
	return out, nil
}

func TranscribeWAV(s Settings, wav []byte, language string) (map[string]any, error) {
	asr := s.JaRepeatASREngine
	if strings.TrimSpace(asr) == "" {
		asr = s.EnglishASREngine
	}
	text, det, err := cloudapi.CloudTranscribe(s.ToCredentials(), asr, s.transcribeModel(asr), language, wav)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"text":       text,
		"asr_engine": asr,
		"asr_model":  s.transcribeModel(asr),
	}
	if det != nil {
		out["detected_language"] = *det
	} else {
		out["detected_language"] = language
	}
	return out, nil
}
