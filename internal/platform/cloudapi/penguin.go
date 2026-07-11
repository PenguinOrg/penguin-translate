package cloudapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

func PenguinTranscribeWAV(creds Credentials, model, language, context string, wav []byte, timeout time.Duration) (text string, detected *string, err error) {
	if len(wav) < 800 {
		return "", nil, nil
	}
	key := strings.TrimSpace(creds.PenguinKey)
	if key == "" {
		return "", nil, fmt.Errorf("Penguin API key required for ASR — sign in to Penguin Cloud")
	}
	base, err := ResolvePenguinBase(creds.PenguinBase)
	if err != nil {
		return "", nil, err
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "penguin/asr"
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("model", model)
	if lang := strings.TrimSpace(language); len(lang) >= 2 {
		if len(lang) > 16 {
			lang = lang[:16]
		}
		_ = mw.WriteField("language", lang)
	}
	if ctx := strings.TrimSpace(context); ctx != "" {
		_ = mw.WriteField("context", ctx)
	}
	fw, _ := mw.CreateFormFile("file", "clip.wav")
	_, _ = fw.Write(wav)
	_ = mw.Close()
	req, err := http.NewRequest(http.MethodPost, base+"/v1/audio/transcriptions", &buf)
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
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("Penguin STT HTTP %d: %s", resp.StatusCode, truncate(string(b), 2000))
	}
	var data struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return "", nil, err
	}
	if d := strings.TrimSpace(language); len(d) >= 2 {
		dd := d[:2]
		detected = &dd
	}
	return strings.TrimSpace(data.Text), detected, nil
}
