package cloudapi

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResolvePenguinProvider(t *testing.T) {
	creds := Credentials{APIProvider: "penguin", PenguinKey: "pgn_k", PenguinBase: "https://pg.test/"}
	key, base, provider, err := creds.resolve()
	if err != nil {
		t.Fatal(err)
	}
	if key != "pgn_k" || provider != "penguin" {
		t.Errorf("key=%q provider=%q, want pgn_k/penguin", key, provider)
	}
	if base != "https://pg.test/v1" {
		t.Errorf("base = %q, want https://pg.test/v1", base)
	}

	if _, _, _, err := (Credentials{APIProvider: "penguin"}).resolve(); err == nil || !strings.Contains(err.Error(), "Penguin") {
		t.Errorf("missing key: err = %v, want Penguin key error", err)
	}
	if _, _, _, err := (Credentials{APIProvider: "penguin", PenguinKey: "pgn_k"}).resolve(); err == nil || !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("missing build endpoint: err = %v, want endpoint configuration error", err)
	}
}

func TestNormalizeASREnginePenguin(t *testing.T) {
	if got := NormalizeASREngine("penguin"); got != "penguin" {
		t.Errorf("NormalizeASREngine(penguin) = %q", got)
	}
	if got := NormalizeASREngine(" Penguin "); got != "penguin" {
		t.Errorf("NormalizeASREngine(' Penguin ') = %q", got)
	}
}

func TestPenguinTranscribeWAVRequestShape(t *testing.T) {
	var gotPath, gotAuth, gotModel, gotLang, gotContext string
	var gotFile []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = r.ParseMultipartForm(1 << 20)
		gotModel = r.FormValue("model")
		gotLang = r.FormValue("language")
		gotContext = r.FormValue("context")
		f, _, err := r.FormFile("file")
		if err == nil {
			gotFile, _ = io.ReadAll(f)
			f.Close()
		}
		_, _ = io.WriteString(w, `{"text":" こんにちは "}`)
	}))
	defer srv.Close()

	wav := make([]byte, 1000)
	wav[0] = 'R'
	creds := Credentials{PenguinKey: "pgn_k", PenguinBase: srv.URL}
	text, detected, err := PenguinTranscribeWAV(creds, "", "ja", "VR vocabulary", wav, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/audio/transcriptions" {
		t.Errorf("path = %q, want /v1/audio/transcriptions", gotPath)
	}
	if gotAuth != "Bearer pgn_k" {
		t.Errorf("Authorization = %q, want Bearer pgn_k", gotAuth)
	}
	if gotModel != "penguin/asr" {
		t.Errorf("model = %q, want penguin/asr (empty model must default)", gotModel)
	}
	if gotLang != "ja" {
		t.Errorf("language = %q, want ja", gotLang)
	}
	if gotContext != "VR vocabulary" {
		t.Errorf("context = %q, want VR vocabulary", gotContext)
	}
	if len(gotFile) != len(wav) || gotFile[0] != 'R' {
		t.Errorf("file field: got %d bytes, want the %d-byte WAV", len(gotFile), len(wav))
	}
	if text != "こんにちは" {
		t.Errorf("text = %q, want trimmed こんにちは", text)
	}
	if detected == nil || *detected != "ja" {
		t.Errorf("detected = %v, want ja", detected)
	}
}

func TestPenguinTranscribeWAVErrors(t *testing.T) {
	wav := make([]byte, 1000)
	if _, _, err := PenguinTranscribeWAV(Credentials{}, "", "ja", "", wav, time.Minute); err == nil || !strings.Contains(err.Error(), "Penguin") {
		t.Errorf("missing key: err = %v, want Penguin key error", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = io.WriteString(w, `{"error":{"code":"credits_exhausted"}}`)
	}))
	defer srv.Close()
	_, _, err := PenguinTranscribeWAV(Credentials{PenguinKey: "k", PenguinBase: srv.URL}, "", "ja", "", wav, time.Minute)
	if err == nil || !strings.Contains(err.Error(), "402") {
		t.Errorf("HTTP 402: err = %v, want surfaced status", err)
	}
}

func TestPenguinAssessRequestShape(t *testing.T) {
	var gotPath, gotQuery, gotAuth, gotPAHeader string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("language")
		gotAuth = r.Header.Get("Authorization")
		gotPAHeader = r.Header.Get("Pronunciation-Assessment")
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, assessmentSuccessBody)
	}))
	defer srv.Close()

	audio := []byte{1, 2, 3, 4}
	res, err := PenguinAssessPronunciation(srv.URL, "pgn_k", "ja-JP", "こんにちは", audio)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/assess" {
		t.Errorf("path = %q, want /v1/assess", gotPath)
	}
	if gotQuery != "ja-JP" {
		t.Errorf("language query = %q, want ja-JP", gotQuery)
	}
	if gotAuth != "Bearer pgn_k" {
		t.Errorf("Authorization = %q, want Bearer pgn_k", gotAuth)
	}
	if string(gotBody) != string(audio) {
		t.Errorf("body = %v, want the raw WAV bytes", gotBody)
	}
	cfgJSON, err := base64.StdEncoding.DecodeString(gotPAHeader)
	if err != nil {
		t.Fatalf("Pronunciation-Assessment header is not base64: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(cfgJSON, &cfg); err != nil {
		t.Fatalf("Pronunciation-Assessment header is not JSON: %v", err)
	}
	if cfg["ReferenceText"] != "こんにちは" {
		t.Errorf("ReferenceText = %v", cfg["ReferenceText"])
	}
	if _, ok := cfg["EnableProsodyAssessment"]; ok {
		t.Error("prosody must not be requested for ja-JP (en-US only)")
	}
	if res.Pron != 95.8 || res.RecognizedText != "The quick brown fox." {
		t.Errorf("parsed result wrong: %+v", res)
	}
}

func TestPenguinAssessValidatesInputs(t *testing.T) {
	audio := []byte{1}
	if _, err := PenguinAssessPronunciation("https://x", "", "en-US", "ref", audio); err == nil || !strings.Contains(err.Error(), "Penguin") {
		t.Errorf("missing key: err = %v", err)
	}
	if _, err := PenguinAssessPronunciation("https://x", "k", "", "ref", audio); err == nil || !strings.Contains(err.Error(), "locale") {
		t.Errorf("missing locale: err = %v", err)
	}
	if _, err := PenguinAssessPronunciation("https://x", "k", "en-US", "ref", nil); err == nil || !strings.Contains(err.Error(), "empty audio") {
		t.Errorf("empty audio: err = %v", err)
	}
}
