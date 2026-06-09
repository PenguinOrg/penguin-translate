package cloudapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func SynthesizeSpeech(creds Credentials, engine, model, voice, instructions, text string) (raw []byte, format string, err error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, "", fmt.Errorf("empty text")
	}
	if len(text) > 4096 {
		text = text[:4096]
	}
	eng := strings.ToLower(strings.TrimSpace(engine))
	if eng == "openai" {
		return openAISpeech(creds, model, voice, instructions, text)
	}
	return openRouterSpeech(creds, model, voice, instructions, text)
}

func openRouterSpeech(creds Credentials, model, voice, instructions, text string) ([]byte, string, error) {
	key := strings.TrimSpace(creds.OpenRouterKey)
	base := strings.TrimSpace(creds.OpenRouterBase)
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	if key == "" {
		return nil, "", fmt.Errorf("OpenRouter API key required for TTS")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "openai/gpt-4o-mini-tts-2025-12-15"
	}
	voice = strings.TrimSpace(voice)
	if voice == "" {
		voice = "coral"
	}
	payload := map[string]any{
		"model":           model,
		"input":           text,
		"voice":           voice,
		"response_format": "pcm",
	}
	if instr := strings.TrimSpace(instructions); instr != "" {
		payload["provider"] = map[string]any{"options": map[string]any{"openai": map[string]any{"instructions": instr}}}
	}
	raw, err := postSpeech(base, key, "openrouter", payload)
	return raw, "pcm", err
}

func openAISpeech(creds Credentials, model, voice, instructions, text string) ([]byte, string, error) {
	key := strings.TrimSpace(creds.OpenAIKey)
	base := strings.TrimSpace(creds.OpenAIBase)
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	if key == "" {
		return nil, "", fmt.Errorf("OpenAI API key required for TTS")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "gpt-4o-mini-tts-2025-12-15"
	}
	voice = strings.TrimSpace(voice)
	if voice == "" {
		voice = "coral"
	}
	payload := map[string]any{
		"model":           model,
		"input":           text,
		"voice":           voice,
		"response_format": "wav",
	}
	if instr := strings.TrimSpace(instructions); instr != "" {
		payload["instructions"] = instr
	}
	raw, err := postSpeech(strings.TrimRight(base, "/"), key, "openai", payload)
	return raw, "wav", err
}

func postSpeech(base, key, provider string, payload map[string]any) ([]byte, error) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, base+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	setHeaders(req, key, provider)
	cl := &http.Client{Timeout: 2 * time.Minute}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("TTS HTTP %d: %s", resp.StatusCode, truncate(string(b), 2000))
	}
	return b, nil
}
