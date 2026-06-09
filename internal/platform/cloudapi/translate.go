package cloudapi

import (
	"strings"
)

func forwardSystemPrompt(kind string) string {
	switch kind {
	case "zh":
		return "You translate English into natural Simplified Chinese for spoken dialogue. Translate the entire input completely; do not summarize, omit, or shorten any part. Output only the Chinese translation, no pinyin, no explanations, no quotes."
	case "ko":
		return "You translate English into natural Korean for spoken dialogue. Translate the entire input completely; do not summarize, omit, or shorten any part. Output only the Korean translation, no romanization, no explanations, no quotes."
	default:
		return "You translate English into natural Japanese for spoken dialogue. Translate the entire input completely; do not summarize, omit, or shorten any part. Output only the Japanese translation, no romaji, no explanations, no quotes."
	}
}

func backSystemPrompt(kind string) string {
	switch kind {
	case "zh":
		return "Translate the following Simplified Chinese into natural English. Output only the English translation."
	case "ko":
		return "Translate the following Korean into natural English. Output only the English translation."
	default:
		return "Translate the following Japanese into natural English. Output only the English translation."
	}
}

func translateSystemPrompt(srcLabel, tgtLabel string) string {
	return "You translate " + srcLabel + " into natural " + tgtLabel +
		" for spoken dialogue. Translate the entire input completely; do not summarize, omit, or shorten any part. " +
		"Output only the " + tgtLabel + " translation, with no transliteration, no explanations, and no quotes."
}

func Translate(creds Credentials, model, srcLabel, tgtLabel, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	return ChatCompletion(creds, model, translateSystemPrompt(srcLabel, tgtLabel), text, 0.2)
}

func TranslateEnglishToTarget(creds Credentials, model, kind, english string) (string, error) {
	english = strings.TrimSpace(english)
	if english == "" {
		return "", nil
	}
	return ChatCompletion(creds, model, forwardSystemPrompt(kind), english, 0.2)
}

func BacktranslateTargetToEnglish(creds Credentials, model, kind, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", nil
	}
	return ChatCompletion(creds, model, backSystemPrompt(kind), target, 0.2)
}

func NormalizeASREngine(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "openai", "openai_gpt", "gpt":
		return "openai"
	case "openai_whisper", "openai-whisper", "whisper-api", "whisper_api":
		return "openai_whisper"
	case "openrouter", "or":
		return "openrouter"
	default:
		return "whisper"
	}
}

func CloudTranscribe(creds Credentials, asrEngine, model, language string, wav []byte) (text string, detected *string, err error) {
	switch NormalizeASREngine(asrEngine) {
	case "openrouter", "or":
		return OpenRouterTranscribeWAV(creds, model, language, wav)
	case "openai", "openai_whisper", "openai-whisper":
		return OpenAITranscribeWAV(creds, model, language, wav)
	default:
		return OpenRouterTranscribeWAV(creds, model, language, wav)
	}
}
