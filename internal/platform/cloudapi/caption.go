package cloudapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"
	"time"
)

func MultimodalCaptionWAV(creds Credentials, model, language string, wav []byte, wantTranslate bool, timeout time.Duration) (text, english, detectedLang string, err error) {
	if len(wav) < 800 {
		return "", "", language, nil
	}
	key, base, provider, err := creds.resolve()
	if err != nil {
		return "", "", "", err
	}
	lang := strings.TrimSpace(language)
	if lang == "" {
		lang = "ja"
	}
	if len(lang) >= 2 {
		lang = lang[:2]
	}
	trNote := `"english" must be an empty string.`
	if wantTranslate {
		trNote = `Include "english" with a natural English translation.`
	}
	sysPrompt := fmt.Sprintf(
		"You transcribe short speech audio. Focus on Chinese (zh), Japanese (ja), or English (en). Language hint: %s. %s Reply with ONLY JSON: {\"text\":\"…\",\"english\":\"…\",\"detected_lang\":\"zh|ja|en\"} — no markdown.",
		lang, trNote,
	)
	b64 := base64.StdEncoding.EncodeToString(wav)
	body := map[string]any{
		"model": strings.TrimSpace(model),
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": sysPrompt},
				{"type": "input_audio", "input_audio": map[string]string{"data": b64, "format": "wav"}},
			},
		}},
		"temperature": 0.1,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, base+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", "", "", err
	}
	setHeaders(req, key, provider)
	cl := &http.Client{Timeout: timeout}
	resp, err := cl.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", "", "", fmt.Errorf("multimodal HTTP %d: %s", resp.StatusCode, truncate(string(b), 2000))
	}
	content, err := chatContentFromResponse(b)
	if err != nil {
		return "", "", "", err
	}
	obj, err := parseJSONObject(content)
	if err != nil {
		return strings.TrimSpace(content), "", lang, nil
	}
	text = strings.TrimSpace(fmt.Sprint(obj["text"]))
	english = strings.TrimSpace(fmt.Sprint(obj["english"]))
	detectedLang = strings.TrimSpace(fmt.Sprint(obj["detected_lang"]))
	if detectedLang == "" {
		detectedLang = lang
	}
	if len(detectedLang) >= 2 {
		detectedLang = detectedLang[:2]
	}
	return text, english, detectedLang, nil
}

func OpenAITranscribeDetailed(creds Credentials, model, language string, wav []byte, diarize bool, timeout time.Duration) (text string, segments []map[string]any, err error) {
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
		model = "gpt-4o-mini-transcribe"
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("model", model)
	if diarize {
		_ = mw.WriteField("response_format", "diarized_json")
		_ = mw.WriteField("chunking_strategy", "auto")
	} else {
		_ = mw.WriteField("response_format", "json")
	}
	if lang := strings.TrimSpace(language); len(lang) >= 2 {
		_ = mw.WriteField("language", lang[:16])
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
	cl := &http.Client{Timeout: timeout}
	resp, err := cl.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("OpenAI STT HTTP %d: %s", resp.StatusCode, truncate(string(b), 2000))
	}
	var data map[string]any
	if err := json.Unmarshal(b, &data); err != nil {
		return "", nil, err
	}
	text = strings.TrimSpace(fmt.Sprint(data["text"]))
	if raw, ok := data["segments"].([]any); ok {
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				segments = append(segments, m)
			}
		}
	}
	return text, segments, nil
}

func BatchTranslateToEN(creds Credentials, model, sourceLang string, lines []string, timeout time.Duration) ([]string, error) {
	cleaned := make([]string, len(lines))
	copy(cleaned, lines)
	if !anyNonEmpty(cleaned) {
		return make([]string, len(lines)), nil
	}
	lang := strings.TrimSpace(sourceLang)
	if len(lang) >= 2 {
		lang = lang[:2]
	}
	langName := map[string]string{"ja": "Japanese", "zh": "Chinese", "ko": "Korean", "en": "English"}[lang]
	if langName == "" {
		langName = lang
	}
	type item struct {
		I int    `json:"i"`
		T string `json:"t"`
	}
	var payload []item
	for i, t := range cleaned {
		payload = append(payload, item{I: i, T: strings.TrimSpace(t)})
	}
	rawPayload, _ := json.Marshal(payload)
	sys := fmt.Sprintf(
		"You translate %s lines to natural English. Input is JSON [{\"i\":number,\"t\":string}]. Output ONLY a JSON array [{\"i\":number,\"en\":string}] in the same order. Escape double quotes inside en strings. No markdown, no commentary.",
		langName,
	)
	raw, err := ChatCompletion(creds, model, sys, string(rawPayload), 0.1)
	if err != nil {
		return nil, err
	}
	parsed := parseTranslationBatch(raw, len(lines))
	if parsed == nil {
		return nil, fmt.Errorf("translation batch parse failed")
	}
	return parsed, nil
}

func anyNonEmpty(lines []string) bool {
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			return true
		}
	}
	return false
}

func chatContentFromResponse(b []byte) (string, error) {
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", err
	}
	if out.Error != nil {
		return "", fmt.Errorf("API error: %v", out.Error)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("empty chat response")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

func parseJSONObject(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty")
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err == nil {
		return obj, nil
	}
	re := regexp.MustCompile(`\{[\s\S]*\}`)
	if m := re.FindString(raw); m != "" {
		if err := json.Unmarshal([]byte(m), &obj); err == nil {
			return obj, nil
		}
	}
	return nil, fmt.Errorf("not json")
}

func parseTranslationBatch(raw string, n int) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	raw = regexp.MustCompile("^```(?:json)?\\s*").ReplaceAllString(raw, "")
	raw = regexp.MustCompile("\\s*```$").ReplaceAllString(raw, "")
	candidates := []string{raw, fixTrailingCommas(raw)}
	if m := regexp.MustCompile(`\[[\s\S]*\]`).FindString(raw); m != "" {
		candidates = append(candidates, m, fixTrailingCommas(m))
	}
	for _, cand := range candidates {
		var parsed any
		if err := json.Unmarshal([]byte(cand), &parsed); err != nil {
			continue
		}
		switch v := parsed.(type) {
		case []any:
			if out := translationsFromArray(v, n); out != nil {
				return out
			}
		case map[string]any:
			for _, key := range []string{"items", "translations", "results", "data"} {
				if inner, ok := v[key].([]any); ok {
					if out := translationsFromArray(inner, n); out != nil {
						return out
					}
				}
			}
		}
	}
	if n == 1 && raw != "" && !strings.HasPrefix(strings.TrimSpace(raw), "[") && !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		return []string{raw}
	}
	return nil
}

func fixTrailingCommas(s string) string {
	return regexp.MustCompile(`,\s*([}\]])`).ReplaceAllString(s, "$1")
}

func translationsFromArray(arr []any, n int) []string {
	out := make([]string, n)
	got := 0
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		idx := -1
		switch v := m["i"].(type) {
		case float64:
			idx = int(v)
		case json.Number:
			i, _ := v.Int64()
			idx = int(i)
		}
		if idx < 0 || idx >= n {
			continue
		}
		en := m["en"]
		if en == nil {
			en = m["english"]
		}
		out[idx] = strings.TrimSpace(fmt.Sprint(en))
		got++
	}
	if got == 0 {
		return nil
	}
	return out
}
