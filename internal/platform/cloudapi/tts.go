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

// SynthesizeSpeech turns text into speech. Returns raw audio bytes + a format
// tag ("wav" | "mp3" | "pcm"); pcm is signed-16-bit-LE mono @ 24kHz.
func SynthesizeSpeech(creds Credentials, engine, model, voice, instructions, text string) (raw []byte, format string, err error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, "", fmt.Errorf("empty text")
	}
	if len(text) > 4096 {
		text = text[:4096]
	}
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "openai":
		return openAISpeech(creds, model, voice, instructions, text)
	case "deepgram":
		return deepgramSpeech(creds, model, text)
	case "dashscope":
		return dashscopeSpeech(creds, model, voice, text)
	default:
		return openRouterSpeech(creds, model, voice, instructions, text)
	}
}

// dashscopeSpeech calls Alibaba Qwen-TTS via DashScope's NATIVE multimodal endpoint
// (it isn't exposed on DashScope's OpenAI-compatible surface). Qwen3-TTS is multilingual
// (Japanese/Chinese/Korean/…). Non-streaming returns a URL to a WAV we then fetch.
func dashscopeSpeech(creds Credentials, model, voice, text string) ([]byte, string, error) {
	key := strings.TrimSpace(creds.DashScopeKey)
	if key == "" {
		return nil, "", fmt.Errorf("DashScope API key required for TTS")
	}
	base := strings.TrimSpace(creds.DashScopeBase)
	if base == "" {
		base = "https://dashscope-intl.aliyuncs.com"
	}
	// The configured base is the OpenAI-compatible URL; the native TTS API lives at the host root.
	base = strings.TrimSuffix(strings.TrimRight(base, "/"), "/compatible-mode/v1")
	model = strings.TrimSpace(model)
	if model == "" {
		model = "qwen3-tts-flash"
	}
	voice = strings.TrimSpace(voice)
	if voice == "" {
		voice = "Cherry"
	}
	reqBody, _ := json.Marshal(map[string]any{
		"model": model,
		"input": map[string]any{"text": text, "voice": voice},
	})
	req, err := http.NewRequest(http.MethodPost, base+"/api/v1/services/aigc/multimodal-generation/generation", bytes.NewReader(reqBody))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	cl := &http.Client{Timeout: 2 * time.Minute}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("DashScope TTS HTTP %d: %s", resp.StatusCode, truncate(string(b), 2000))
	}
	var out struct {
		Output struct {
			Audio struct {
				URL string `json:"url"`
			} `json:"audio"`
		} `json:"output"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, "", fmt.Errorf("DashScope TTS: bad response: %w", err)
	}
	if out.Code != "" {
		return nil, "", fmt.Errorf("DashScope TTS: %s", out.Message)
	}
	if out.Output.Audio.URL == "" {
		return nil, "", fmt.Errorf("DashScope TTS: no audio returned")
	}
	audio, err := http.Get(out.Output.Audio.URL)
	if err != nil {
		return nil, "", err
	}
	defer audio.Body.Close()
	wav, _ := io.ReadAll(io.LimitReader(audio.Body, 8<<20))
	if audio.StatusCode >= 400 || len(wav) == 0 {
		return nil, "", fmt.Errorf("DashScope TTS: fetch audio failed (HTTP %d)", audio.StatusCode)
	}
	return wav, "wav", nil
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
		model = "google/gemini-3.1-flash-tts-preview"
	}
	voice = strings.TrimSpace(voice)
	if voice == "" {
		voice = "Kore"
	}
	payload := map[string]any{
		"model":           model,
		"input":           text,
		"voice":           voice,
		"response_format": "pcm", // Gemini TTS on OpenRouter only accepts pcm; wrapped to WAV by the caller
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
		model = "gpt-4o-mini-tts"
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

// deepgramSpeech calls Deepgram Aura (/v1/speak). The "model" is the combined
// voice id (e.g. aura-2-thalia-en); Aura voices are English. Default output is mp3.
func deepgramSpeech(creds Credentials, model, text string) ([]byte, string, error) {
	key := strings.TrimSpace(creds.DeepgramKey)
	base := strings.TrimSpace(creds.DeepgramBase)
	if base == "" {
		base = "https://api.deepgram.com"
	}
	if key == "" {
		return nil, "", fmt.Errorf("Deepgram API key required for TTS")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "aura-2-thalia-en"
	}
	if len(text) > 2000 { // Aura caps a single request at 2000 chars
		text = text[:2000]
	}
	body, _ := json.Marshal(map[string]any{"text": text})
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+"/v1/speak?model="+model, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Token "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/mpeg")
	cl := &http.Client{Timeout: 2 * time.Minute}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("Deepgram TTS HTTP %d: %s", resp.StatusCode, truncate(string(b), 2000))
	}
	return b, "mp3", nil
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
