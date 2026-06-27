package cloudapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

type Credentials struct {
	OpenAIKey      string
	OpenAIBase     string
	OpenRouterKey  string
	OpenRouterBase string
	DashScopeKey   string
	DashScopeBase  string
	APIProvider    string
}

const dashScopeDefaultBase = "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"

func (c Credentials) resolve() (key, base, provider string, err error) {
	p := strings.ToLower(strings.TrimSpace(c.APIProvider))
	if p == "" {
		p = "openrouter"
	}
	switch p {
	case "openai":
		key = strings.TrimSpace(c.OpenAIKey)
		base = strings.TrimSpace(c.OpenAIBase)
		if base == "" {
			base = "https://api.openai.com/v1"
		}
		if key == "" {
			return "", "", "", fmt.Errorf("OpenAI API key required")
		}
		return key, strings.TrimRight(base, "/"), "openai", nil
	case "dashscope":
		key = strings.TrimSpace(c.DashScopeKey)
		base = strings.TrimSpace(c.DashScopeBase)
		if base == "" {
			base = dashScopeDefaultBase
		}
		if key == "" {
			return "", "", "", fmt.Errorf("DashScope API key required")
		}
		return key, strings.TrimRight(base, "/"), "dashscope", nil
	default:
		key = strings.TrimSpace(c.OpenRouterKey)
		base = strings.TrimSpace(c.OpenRouterBase)
		if base == "" {
			base = "https://openrouter.ai/api/v1"
		}
		if key == "" {
			return "", "", "", fmt.Errorf("OpenRouter API key required")
		}
		return key, strings.TrimRight(base, "/"), "openrouter", nil
	}
}

func setHeaders(req *http.Request, key, provider string) {
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	if provider == "openrouter" {
		req.Header.Set("HTTP-Referer", "https://translation-overlay")
		req.Header.Set("X-Title", "Penguin Translate")
	}
}

type ChatOptions struct {
	Temperature    float64
	ResponseFormat string
	MaxTokens      int
}

func ChatCompletion(creds Credentials, model, system, user string, temperature float64) (string, error) {
	return ChatCompletionWith(creds, model, system, user, ChatOptions{Temperature: temperature})
}

func ChatCompletionWith(creds Credentials, model, system, user string, opts ChatOptions) (string, error) {
	key, base, provider, err := creds.resolve()
	if err != nil {
		return "", err
	}
	model = strings.TrimSpace(model)
	if model == "" {
		if provider == "openrouter" {
			model = "openai/gpt-4o-mini"
		} else {
			model = "gpt-4o-mini"
		}
	}
	body := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}
	if opts.ResponseFormat != "" {
		body["response_format"] = map[string]string{"type": opts.ResponseFormat}
	}
	if opts.MaxTokens > 0 && useMaxCompletionTokens(model) {
		body["max_completion_tokens"] = opts.MaxTokens
	} else {
		body["temperature"] = opts.Temperature
		if opts.MaxTokens > 0 {
			body["max_tokens"] = opts.MaxTokens
		}
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, base+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	setHeaders(req, key, provider)
	cl := &http.Client{Timeout: 3 * time.Minute}
	resp, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("chat HTTP %d: %s", resp.StatusCode, truncate(string(b), 2000))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("empty chat response")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

func OpenRouterTranscribeWAV(creds Credentials, model, language string, wav []byte) (text string, detected *string, err error) {
	if len(wav) < 800 {
		return "", nil, nil
	}
	key := strings.TrimSpace(creds.OpenRouterKey)
	base := strings.TrimSpace(creds.OpenRouterBase)
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	if key == "" {
		return "", nil, fmt.Errorf("OpenRouter API key required for ASR")
	}
	base = strings.TrimRight(base, "/")
	model = strings.TrimSpace(model)
	if model == "" {
		model = "qwen/qwen3-asr-flash-2026-02-10"
	}
	payload := map[string]any{
		"model": model,
		"input_audio": map[string]string{
			"data":   base64.StdEncoding.EncodeToString(wav),
			"format": "wav",
		},
	}
	if lang := strings.TrimSpace(language); len(lang) >= 2 {
		payload["language"] = lang[:2]
	}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, base+"/audio/transcriptions", bytes.NewReader(raw))
	if err != nil {
		return "", nil, err
	}
	setHeaders(req, key, "openrouter")
	cl := &http.Client{Timeout: 3 * time.Minute}
	resp, err := cl.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("OpenRouter STT HTTP %d: %s", resp.StatusCode, truncate(string(b), 2000))
	}
	var data map[string]any
	if err := json.Unmarshal(b, &data); err != nil {
		return "", nil, err
	}
	text = strings.TrimSpace(fmt.Sprint(data["text"]))
	if d, ok := data["language"].(string); ok && strings.TrimSpace(d) != "" {
		d = strings.TrimSpace(d)
		detected = &d
	}
	return text, detected, nil
}

func OpenAITranscribeWAV(creds Credentials, model, language string, wav []byte) (text string, detected *string, err error) {
	if len(wav) < 800 {
		return "", nil, nil
	}
	key := strings.TrimSpace(creds.OpenAIKey)
	if key == "" {
		return "", nil, fmt.Errorf("OpenAI API key required for ASR")
	}
	base := strings.TrimSpace(creds.OpenAIBase)
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	base = strings.TrimRight(base, "/")
	model = strings.TrimSpace(model)
	if model == "" {
		model = "whisper-1"
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("model", model)
	if strings.HasPrefix(model, "gpt-4o") || strings.HasPrefix(model, "gpt-4-turbo") || strings.HasPrefix(model, "whisper") {
		_ = mw.WriteField("response_format", "json")
	}
	if lang := strings.TrimSpace(language); len(lang) >= 2 {
		if len(lang) > 16 {
			lang = lang[:16]
		}
		_ = mw.WriteField("language", lang)
	}
	fw, _ := mw.CreateFormFile("file", "clip.wav")
	_, _ = fw.Write(wav)
	_ = mw.Close()
	req, err := http.NewRequest(http.MethodPost, base+"/audio/transcriptions", &buf)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	cl := &http.Client{Timeout: 3 * time.Minute}
	resp, err := cl.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("OpenAI STT HTTP %d: %s", resp.StatusCode, truncate(string(b), 2000))
	}
	var data map[string]any
	if err := json.Unmarshal(b, &data); err != nil {
		return "", nil, err
	}
	text = strings.TrimSpace(fmt.Sprint(data["text"]))
	if d, ok := data["language"].(string); ok && strings.TrimSpace(d) != "" {
		d = strings.TrimSpace(d)
		detected = &d
	}
	return text, detected, nil
}

func useMaxCompletionTokens(model string) bool {
	m := strings.ToLower(strings.ReplaceAll(model, " ", ""))
	return strings.Contains(m, "gpt-4o") || strings.Contains(m, "gpt-5") ||
		strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
