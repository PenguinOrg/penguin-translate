package cloudapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AzurePAWord is a single word's pronunciation verdict.
type AzurePAWord struct {
	Word      string  `json:"word"`
	Accuracy  float64 `json:"accuracy"`
	ErrorType string  `json:"error_type"` // None | Mispronunciation | Omission | Insertion
}

// AzurePAResult is the parsed pronunciation-assessment outcome.
type AzurePAResult struct {
	RecognizedText string        `json:"recognized_text"`
	Accuracy       float64       `json:"accuracy"`
	Fluency        float64       `json:"fluency"`
	Completeness   float64       `json:"completeness"`
	Prosody        float64       `json:"prosody"`
	Pron           float64       `json:"pron"`
	Words          []AzurePAWord `json:"words"`
}

type azurePAResponse struct {
	RecognitionStatus string `json:"RecognitionStatus"`
	DisplayText       string `json:"DisplayText"`
	// REST responses expose scores directly on NBest and Words.
	NBest []struct {
		Display           string  `json:"Display"`
		Lexical           string  `json:"Lexical"`
		AccuracyScore     float64 `json:"AccuracyScore"`
		FluencyScore      float64 `json:"FluencyScore"`
		CompletenessScore float64 `json:"CompletenessScore"`
		ProsodyScore      float64 `json:"ProsodyScore"`
		PronScore         float64 `json:"PronScore"`
		Words             []struct {
			Word          string  `json:"Word"`
			AccuracyScore float64 `json:"AccuracyScore"`
			ErrorType     string  `json:"ErrorType"`
		} `json:"Words"`
	} `json:"NBest"`
}

// AssessPronunciation scores up to 30 seconds of 16 kHz mono PCM WAV audio.
// Silence and no-match responses return a zero result without an error.
func AssessPronunciation(region, key, locale, referenceText string, audio []byte) (AzurePAResult, error) {
	region = strings.TrimSpace(region)
	key = strings.TrimSpace(key)
	if region == "" {
		return AzurePAResult{}, fmt.Errorf("Azure Speech region required")
	}
	if key == "" {
		return AzurePAResult{}, fmt.Errorf("Azure Speech key required")
	}
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return AzurePAResult{}, fmt.Errorf("locale required")
	}
	if len(audio) == 0 {
		return AzurePAResult{}, fmt.Errorf("empty audio")
	}

	cfg := map[string]any{
		"ReferenceText": referenceText,
		"GradingSystem": "HundredMark",
		"Granularity":   "Word",
		"Dimension":     "Comprehensive",
		"EnableMiscue":  "True",
	}
	// Prosody assessment is en-US only; requesting it elsewhere is rejected.
	if strings.EqualFold(locale, "en-US") {
		cfg["EnableProsodyAssessment"] = "True"
	}
	cfgJSON, _ := json.Marshal(cfg)
	cfgHeader := base64.StdEncoding.EncodeToString(cfgJSON)

	url := fmt.Sprintf("https://%s.stt.speech.microsoft.com/speech/recognition/conversation/cognitiveservices/v1?language=%s&format=detailed", region, locale)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(audio))
	if err != nil {
		return AzurePAResult{}, err
	}
	req.Header.Set("Ocp-Apim-Subscription-Key", key)
	req.Header.Set("Content-Type", "audio/wav; codecs=audio/pcm; samplerate=16000")
	req.Header.Set("Pronunciation-Assessment", cfgHeader)
	req.Header.Set("Accept", "application/json")

	cl := &http.Client{Timeout: 30 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return AzurePAResult{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return AzurePAResult{}, fmt.Errorf("Azure assessment HTTP %d: %s", resp.StatusCode, truncate(string(b), 600))
	}

	return parseAzurePA(b)
}

// parseAzurePA parses Azure's detailed REST response.
func parseAzurePA(b []byte) (AzurePAResult, error) {
	var raw azurePAResponse
	if err := json.Unmarshal(b, &raw); err != nil {
		return AzurePAResult{}, fmt.Errorf("Azure assessment: bad response: %w", err)
	}
	if !strings.EqualFold(raw.RecognitionStatus, "Success") || len(raw.NBest) == 0 {
		return AzurePAResult{}, nil
	}
	n := raw.NBest[0]
	out := AzurePAResult{
		RecognizedText: strings.TrimSpace(firstNonEmpty(n.Display, n.Lexical, raw.DisplayText)),
		Accuracy:       n.AccuracyScore,
		Fluency:        n.FluencyScore,
		Completeness:   n.CompletenessScore,
		Prosody:        n.ProsodyScore,
		Pron:           n.PronScore,
	}
	for _, w := range n.Words {
		out.Words = append(out.Words, AzurePAWord{
			Word:      w.Word,
			Accuracy:  w.AccuracyScore,
			ErrorType: w.ErrorType,
		})
	}
	return out, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
