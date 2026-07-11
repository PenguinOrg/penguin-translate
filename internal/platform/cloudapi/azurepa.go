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

// PronunciationAssessmentWord is a single word's pronunciation verdict.
type PronunciationAssessmentWord struct {
	Word      string  `json:"word"`
	Accuracy  float64 `json:"accuracy"`
	ErrorType string  `json:"error_type"` // None | Mispronunciation | Omission | Insertion
}

// PronunciationAssessmentResult is the parsed pronunciation-assessment outcome.
type PronunciationAssessmentResult struct {
	RecognizedText string                        `json:"recognized_text"`
	Accuracy       float64                       `json:"accuracy"`
	Fluency        float64                       `json:"fluency"`
	Completeness   float64                       `json:"completeness"`
	Prosody        float64                       `json:"prosody"`
	Pron           float64                       `json:"pron"`
	Words          []PronunciationAssessmentWord `json:"words"`
}

type assessmentResponse struct {
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
func AssessPronunciation(region, key, locale, referenceText string, audio []byte) (PronunciationAssessmentResult, error) {
	region = strings.TrimSpace(region)
	key = strings.TrimSpace(key)
	if region == "" {
		return PronunciationAssessmentResult{}, fmt.Errorf("Azure Speech region required")
	}
	if key == "" {
		return PronunciationAssessmentResult{}, fmt.Errorf("Azure Speech key required")
	}
	url := fmt.Sprintf("https://%s.stt.speech.microsoft.com/speech/recognition/conversation/cognitiveservices/v1?language=%s&format=detailed", region, locale)
	return assessPronunciationAt("Azure", url, func(req *http.Request) {
		req.Header.Set("Ocp-Apim-Subscription-Key", key)
	}, locale, referenceText, audio)
}

// PenguinAssessPronunciation submits pronunciation assessment through Penguin Cloud.
func PenguinAssessPronunciation(base, key, locale, referenceText string, audio []byte) (PronunciationAssessmentResult, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return PronunciationAssessmentResult{}, fmt.Errorf("Penguin API key required — sign in to Penguin Cloud")
	}
	base, err := ResolvePenguinBase(base)
	if err != nil {
		return PronunciationAssessmentResult{}, err
	}
	url := base + "/v1/assess?language=" + locale
	return assessPronunciationAt("Penguin", url, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+key)
	}, locale, referenceText, audio)
}

func assessPronunciationAt(provider, url string, setAuth func(*http.Request), locale, referenceText string, audio []byte) (PronunciationAssessmentResult, error) {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return PronunciationAssessmentResult{}, fmt.Errorf("locale required")
	}
	if len(audio) == 0 {
		return PronunciationAssessmentResult{}, fmt.Errorf("empty audio")
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

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(audio))
	if err != nil {
		return PronunciationAssessmentResult{}, err
	}
	setAuth(req)
	req.Header.Set("Content-Type", "audio/wav; codecs=audio/pcm; samplerate=16000")
	req.Header.Set("Pronunciation-Assessment", cfgHeader)
	req.Header.Set("Accept", "application/json")

	cl := &http.Client{Timeout: 30 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return PronunciationAssessmentResult{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return PronunciationAssessmentResult{}, fmt.Errorf("%s assessment HTTP %d: %s", provider, resp.StatusCode, truncate(string(b), 600))
	}

	return parsePronunciationAssessment(b)
}

func parsePronunciationAssessment(b []byte) (PronunciationAssessmentResult, error) {
	var raw assessmentResponse
	if err := json.Unmarshal(b, &raw); err != nil {
		return PronunciationAssessmentResult{}, fmt.Errorf("pronunciation assessment: bad response: %w", err)
	}
	if !strings.EqualFold(raw.RecognitionStatus, "Success") || len(raw.NBest) == 0 {
		return PronunciationAssessmentResult{}, nil
	}
	n := raw.NBest[0]
	out := PronunciationAssessmentResult{
		RecognizedText: strings.TrimSpace(firstNonEmpty(n.Display, n.Lexical, raw.DisplayText)),
		Accuracy:       n.AccuracyScore,
		Fluency:        n.FluencyScore,
		Completeness:   n.CompletenessScore,
		Prosody:        n.ProsodyScore,
		Pron:           n.PronScore,
	}
	for _, w := range n.Words {
		out.Words = append(out.Words, PronunciationAssessmentWord{
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
