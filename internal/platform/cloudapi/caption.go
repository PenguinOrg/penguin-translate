package cloudapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func MultimodalCaptionWAV(creds Credentials, model, language, context string, wav []byte, wantTranslate bool, timeout time.Duration) (text, english, detectedLang string, err error) {
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
	trNote := `Set "english" to an empty string.`
	if wantTranslate {
		trNote = `Set "english" to a natural English translation of the speech.`
	}
	sysPrompt := fmt.Sprintf(
		"You are a speech transcription engine, not a chat assistant. Transcribe the spoken audio verbatim. "+
			"Focus on Chinese (zh), Japanese (ja), or English (en). Language hint: %s. %s "+
			"If the audio has no intelligible human speech (silence, noise, music, or a non-speech fragment), "+
			"set \"speech\" to false and make \"text\" and \"english\" empty. "+
			"Never reply, apologize, ask a question, or write any sentence addressed to a person. "+
			"Output ONLY this JSON object and nothing else: "+
			"{\"speech\":true|false,\"text\":\"…\",\"english\":\"…\",\"detected_lang\":\"zh|ja|en|other\"}",
		lang, trNote,
	)
	b64 := base64.StdEncoding.EncodeToString(wav)
	messages := []map[string]any{}
	if ctx := strings.TrimSpace(context); ctx != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": []map[string]any{{"type": "text", "text": ctx}},
		})
	}
	messages = append(messages, map[string]any{
		"role": "user",
		"content": []map[string]any{
			{"type": "text", "text": sysPrompt},
			{"type": "input_audio", "input_audio": map[string]string{"data": b64, "format": "wav"}},
		},
	})
	body := map[string]any{
		"model":       strings.TrimSpace(model),
		"messages":    messages,
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
	if v, ok := obj["speech"]; ok {
		switch t := v.(type) {
		case bool:
			if !t {
				return "", "", lang, nil
			}
		case string:
			if strings.EqualFold(strings.TrimSpace(t), "false") {
				return "", "", lang, nil
			}
		}
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

func DashScopeTranscribeWAV(creds Credentials, model, language, context string, wav []byte, timeout time.Duration) (text string, detected *string, err error) {
	if len(wav) < 800 {
		return "", nil, nil
	}
	key := strings.TrimSpace(creds.DashScopeKey)
	base := strings.TrimSpace(creds.DashScopeBase)
	if base == "" {
		base = dashScopeDefaultBase
	}
	if key == "" {
		return "", nil, fmt.Errorf("DashScope API key required for ASR")
	}
	base = strings.TrimRight(base, "/")
	model = strings.TrimSpace(model)
	if model == "" {
		model = "qwen3-asr-flash"
	}
	b64 := base64.StdEncoding.EncodeToString(wav)
	messages := []map[string]any{}
	if ctx := strings.TrimSpace(context); ctx != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": []map[string]any{{"type": "text", "text": ctx}},
		})
	}
	messages = append(messages, map[string]any{
		"role": "user",
		"content": []map[string]any{
			{"type": "input_audio", "input_audio": map[string]string{
				"data":   "data:audio/wav;base64," + b64,
				"format": "wav",
			}},
		},
	})
	raw, _ := json.Marshal(map[string]any{"model": model, "messages": messages})
	req, err := http.NewRequest(http.MethodPost, base+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", nil, err
	}
	setHeaders(req, key, "dashscope")
	cl := &http.Client{Timeout: timeout}
	resp, err := cl.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("DashScope STT HTTP %d: %s", resp.StatusCode, truncate(string(b), 2000))
	}
	content, err := chatContentFromResponse(b)
	if err != nil {
		return "", nil, err
	}
	if d := strings.TrimSpace(language); len(d) >= 2 {
		dd := d[:2]
		detected = &dd
	}
	return strings.TrimSpace(content), detected, nil
}

// DeepgramTranscribeWAV transcribes a WAV clip with Deepgram's pre-recorded
// (batch) Speech-to-Text endpoint. Deepgram is ASR-only — it does not translate,
// so callers run the translate step through a separate chat provider.
func DeepgramTranscribeWAV(creds Credentials, model, language string, wav []byte, timeout time.Duration) (text string, detected *string, err error) {
	if len(wav) < 800 {
		return "", nil, nil
	}
	key := strings.TrimSpace(creds.DeepgramKey)
	if key == "" {
		return "", nil, fmt.Errorf("Deepgram API key required for ASR")
	}
	base := strings.TrimSpace(creds.DeepgramBase)
	if base == "" {
		base = deepgramDefaultBase
	}
	base = strings.TrimRight(base, "/")
	model = strings.TrimSpace(model)
	if model == "" {
		model = "nova-2"
	}
	q := url.Values{}
	q.Set("model", model)
	q.Set("smart_format", "true")
	q.Set("punctuate", "true")
	if lang := strings.TrimSpace(language); len(lang) >= 2 {
		q.Set("language", lang[:2])
	} else {
		q.Set("detect_language", "true")
	}
	req, err := http.NewRequest(http.MethodPost, base+"/v1/listen?"+q.Encode(), bytes.NewReader(wav))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Authorization", "Token "+key)
	req.Header.Set("Content-Type", "audio/wav")
	cl := &http.Client{Timeout: timeout}
	resp, err := cl.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("Deepgram STT HTTP %d: %s", resp.StatusCode, truncate(string(b), 2000))
	}
	var data struct {
		Results struct {
			Channels []struct {
				DetectedLanguage string `json:"detected_language"`
				Alternatives     []struct {
					Transcript string `json:"transcript"`
				} `json:"alternatives"`
			} `json:"channels"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return "", nil, err
	}
	if len(data.Results.Channels) == 0 || len(data.Results.Channels[0].Alternatives) == 0 {
		return "", nil, nil
	}
	ch := data.Results.Channels[0]
	text = strings.TrimSpace(ch.Alternatives[0].Transcript)
	if d := strings.TrimSpace(ch.DetectedLanguage); d != "" {
		if len(d) > 2 {
			d = d[:2]
		}
		detected = &d
	} else if d := strings.TrimSpace(language); len(d) >= 2 {
		dd := d[:2]
		detected = &dd
	}
	return text, detected, nil
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

func BatchTranslateToEN(creds Credentials, model, sourceLang, extraContext string, lines []string, timeout time.Duration) ([]string, error) {
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
	if c := strings.TrimSpace(extraContext); c != "" {
		sys += " Background context — proper nouns, VR/gaming vocabulary, and recent dialogue. Do NOT translate this; use it only to keep names, terms, and tone consistent: " + c
	}
	raw, err := ChatCompletion(creds, model, sys, string(rawPayload), 0.1)
	if err != nil {
		return nil, err
	}
	parsed := parseTranslationBatch(raw, len(lines))
	if parsed == nil {
		log.Printf("caption: translation batch parse failed model=%q source=%q lines=%d response=%s", model, lang, len(lines), truncate(raw, 1000))
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
			if out := translationsFromMap(v, n); out != nil {
				return out
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
	used := make([]bool, n)
	got := 0
	for _, item := range arr {
		idx := -1
		text := ""
		if m, ok := item.(map[string]any); ok {
			idx = translationIndex(m["i"])
			text, _ = translationText(m)
		} else if v, ok := item.(string); ok {
			text = strings.TrimSpace(v)
		}
		if idx < 0 {
			for idx = 0; idx < n && used[idx]; idx++ {
			}
		}
		if idx < 0 || idx >= n || used[idx] || text == "" {
			continue
		}
		out[idx] = text
		used[idx] = true
		got++
	}
	if got == 0 {
		return nil
	}
	return out
}

func translationIndex(v any) int {
	switch v := v.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return -1
		}
		return int(i)
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return -1
		}
		return i
	default:
		return -1
	}
}

func translationText(m map[string]any) (string, bool) {
	for _, key := range []string{"en", "english", "translation", "translated", "text", "target"} {
		if value, ok := m[key]; ok && value != nil {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text, true
			}
		}
	}
	return "", false
}

func translationsFromMap(m map[string]any, n int) []string {
	out := make([]string, n)
	got := 0
	for key, value := range m {
		idx, err := strconv.Atoi(strings.TrimSpace(key))
		if err != nil || idx < 0 || idx >= n || strings.TrimSpace(out[idx]) != "" {
			continue
		}
		text := ""
		if nested, ok := value.(map[string]any); ok {
			text, _ = translationText(nested)

		} else if raw, ok := value.(string); ok {
			text = strings.TrimSpace(raw)
		}
		if text == "" {
			continue
		}
		out[idx] = text
		got++
	}
	if got == 0 {
		return nil
	}
	return out
}
