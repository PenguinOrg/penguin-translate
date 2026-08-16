package cloudapi

import (
	"strings"
	"time"
)

func forwardSystemPrompt(targetID, targetLabel string) string {
	instruction := ""
	switch targetID {
	case "zh":
		instruction = " Use Simplified Chinese characters."
	case "zh-tw":
		instruction = " Use Traditional Chinese characters (繁體中文)."
	case "yue":
		instruction = " Use natural written Cantonese in Traditional Chinese characters."
	case "wuu":
		instruction = " Use natural written Wu Chinese in Chinese characters."
	}
	return "You translate English into natural " + targetLabel + " for spoken dialogue." + instruction +
		" Translate the entire input completely; do not summarize, omit, or shorten any part. " +
		"Output only the " + targetLabel + " translation, with no transliteration, no explanations, and no quotes."
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

func TranslateEnglishToTarget(creds Credentials, model, targetID, targetLabel, english string) (string, error) {
	english = strings.TrimSpace(english)
	if english == "" {
		return "", nil
	}
	return ChatCompletion(creds, model, forwardSystemPrompt(targetID, targetLabel), english, 0.2)
}

func BacktranslateTargetToEnglish(creds Credentials, model, sourceLabel, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", nil
	}
	return ChatCompletion(creds, model, translateSystemPrompt(sourceLabel, "English"), target, 0.2)
}

func NormalizeASREngine(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "openai", "openai_gpt", "gpt":
		return "openai"
	case "openai_whisper", "openai-whisper", "whisper-api", "whisper_api":
		return "openai_whisper"
	case "openrouter", "or":
		return "openrouter"
	case "deepgram", "dg":
		return "deepgram"
	case "dashscope", "ds":
		return "dashscope"
	case "penguin":
		return "penguin"
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
	case "deepgram":
		return DeepgramTranscribeWAV(creds, model, language, wav, 3*time.Minute)
	case "dashscope":
		return DashScopeTranscribeWAV(creds, model, language, "", wav, 3*time.Minute)
	case "penguin":
		return PenguinTranscribeWAV(creds, model, language, "", wav, 3*time.Minute)
	default:
		return OpenRouterTranscribeWAV(creds, model, language, wav)
	}
}
