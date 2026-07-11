package cloudapi

import (
	"strings"
	"testing"
)

func TestAssessPronunciationValidatesInputs(t *testing.T) {
	audio := []byte{1, 2, 3}
	cases := []struct {
		name                         string
		region, key, locale, wantErr string
		audio                        []byte
	}{
		{"no region", "", "k", "en-US", "region", audio},
		{"no key", "eastus", "", "en-US", "key", audio},
		{"no locale", "eastus", "k", "", "locale", audio},
		{"empty audio", "eastus", "k", "en-US", "empty audio", nil},
	}
	for _, c := range cases {
		_, err := AssessPronunciation(c.region, c.key, c.locale, "ref", c.audio)
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: err = %v, want contains %q", c.name, err, c.wantErr)
		}
	}
}

// Captured from Azure's detailed short-audio REST response.
const assessmentSuccessBody = `{
  "RecognitionStatus": "Success",
  "DisplayText": "The quick brown fox.",
  "NBest": [{
    "Display": "The quick brown fox.",
    "Lexical": "the quick brown fox",
    "AccuracyScore": 97.0,
    "FluencyScore": 100.0,
    "CompletenessScore": 100.0,
    "ProsodyScore": 91.1,
    "PronScore": 95.8,
    "Words": [
      {"Word": "the", "AccuracyScore": 91.0, "ErrorType": "None"},
      {"Word": "quick", "AccuracyScore": 97.0, "ErrorType": "None"},
      {"Word": "brown", "AccuracyScore": 100.0, "ErrorType": "None"},
      {"Word": "fox", "AccuracyScore": 60.0, "ErrorType": "Mispronunciation"}
    ]
  }]
}`

func TestParsePronunciationAssessmentFlatScores(t *testing.T) {
	got, err := parsePronunciationAssessment([]byte(assessmentSuccessBody))
	if err != nil {
		t.Fatalf("parsePronunciationAssessment: %v", err)
	}
	if got.Accuracy != 97 || got.Fluency != 100 || got.Completeness != 100 || got.Pron != 95.8 || got.Prosody != 91.1 {
		t.Fatalf("overall scores not parsed from flat NBest fields: %+v", got)
	}
	if got.RecognizedText != "The quick brown fox." {
		t.Fatalf("RecognizedText = %q", got.RecognizedText)
	}
	if len(got.Words) != 4 {
		t.Fatalf("want 4 words, got %d", len(got.Words))
	}
	if got.Words[3].Word != "fox" || got.Words[3].Accuracy != 60 || got.Words[3].ErrorType != "Mispronunciation" {
		t.Fatalf("per-word flat parse wrong: %+v", got.Words[3])
	}
}

func TestParsePronunciationAssessmentSilence(t *testing.T) {
	got, err := parsePronunciationAssessment([]byte(`{"RecognitionStatus":"InitialSilenceTimeout","NBest":[]}`))
	if err != nil {
		t.Fatalf("silence should not error: %v", err)
	}
	if got.RecognizedText != "" || got.Accuracy != 0 || len(got.Words) != 0 {
		t.Fatalf("silence should yield empty result, got %+v", got)
	}
}
