package cloudapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func captureChat(t *testing.T) (creds Credentials, path *string, body *map[string]any, close func()) {
	t.Helper()
	var gotPath string
	gotBody := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	creds = Credentials{APIProvider: "openai", OpenAIKey: "k", OpenAIBase: srv.URL}
	return creds, &gotPath, &gotBody, srv.Close
}

func TestChatCompletionWithBuildsBody(t *testing.T) {
	t.Run("json mode + max_completion_tokens for gpt-4o", func(t *testing.T) {
		creds, path, body, done := captureChat(t)
		defer done()
		if _, err := ChatCompletionWith(creds, "gpt-4o-mini", "sys", "user", ChatOptions{
			Temperature: 0.2, ResponseFormat: "json_object", MaxTokens: 2000,
		}); err != nil {
			t.Fatal(err)
		}
		if *path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", *path)
		}
		if (*body)["max_completion_tokens"] != float64(2000) {
			t.Errorf("max_completion_tokens = %v, want 2000", (*body)["max_completion_tokens"])
		}
		if _, ok := (*body)["temperature"]; ok {
			t.Error("temperature must be omitted for max_completion_tokens models")
		}
		if _, ok := (*body)["max_tokens"]; ok {
			t.Error("max_tokens must not be set for gpt-4o")
		}
		rf, _ := (*body)["response_format"].(map[string]any)
		if rf["type"] != "json_object" {
			t.Errorf("response_format = %v, want type=json_object", (*body)["response_format"])
		}
	})

	t.Run("max_tokens + temperature for a plain model", func(t *testing.T) {
		creds, _, body, done := captureChat(t)
		defer done()
		if _, err := ChatCompletionWith(creds, "some/plain-model", "sys", "user", ChatOptions{
			Temperature: 0.5, MaxTokens: 100,
		}); err != nil {
			t.Fatal(err)
		}
		if (*body)["max_tokens"] != float64(100) {
			t.Errorf("max_tokens = %v, want 100", (*body)["max_tokens"])
		}
		if (*body)["temperature"] != float64(0.5) {
			t.Errorf("temperature = %v, want 0.5", (*body)["temperature"])
		}
		if _, ok := (*body)["response_format"]; ok {
			t.Error("response_format must be omitted when unset")
		}
	})
}

func TestOpenAITranscribeWAVLanguageField(t *testing.T) {
	var gotLang string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		gotLang = r.FormValue("language")
		_, _ = io.WriteString(w, `{"text":"hello","language":"en"}`)
	}))
	defer srv.Close()
	creds := Credentials{APIProvider: "openai", OpenAIKey: "k", OpenAIBase: srv.URL}
	wav := make([]byte, 1000)

	for _, lang := range []string{"en", "ja", "zh", "yue", "this-is-a-very-long-code"} {
		text, _, err := OpenAITranscribeWAV(creds, "whisper-1", lang, wav)
		if err != nil {
			t.Fatalf("lang %q: %v", lang, err)
		}
		if text != "hello" {
			t.Fatalf("lang %q: text = %q, want hello", lang, text)
		}
		if len(gotLang) > 16 {
			t.Errorf("lang %q: server saw %q (>16 chars, not capped)", lang, gotLang)
		}
	}
}

func TestOpenAITranscribeDetailedLanguageField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		_, _ = io.WriteString(w, `{"text":"hi"}`)
	}))
	defer srv.Close()
	creds := Credentials{APIProvider: "openai", OpenAIKey: "k", OpenAIBase: srv.URL}
	wav := make([]byte, 1000)
	for _, lang := range []string{"en", "ja", "zh"} {
		text, _, err := OpenAITranscribeDetailed(creds, "gpt-4o-mini-transcribe", lang, wav, false, time.Minute)
		if err != nil {
			t.Fatalf("lang %q: %v", lang, err)
		}
		if text != "hi" {
			t.Fatalf("lang %q: text = %q, want hi", lang, text)
		}
	}
}

func TestChatCompletionWrapperUnchanged(t *testing.T) {
	creds, _, body, done := captureChat(t)
	defer done()
	if _, err := ChatCompletion(creds, "gpt-4o-mini", "sys", "user", 0.3); err != nil {
		t.Fatal(err)
	}
	if (*body)["temperature"] != float64(0.3) {
		t.Errorf("temperature = %v, want 0.3", (*body)["temperature"])
	}
	for _, k := range []string{"max_tokens", "max_completion_tokens", "response_format"} {
		if _, ok := (*body)[k]; ok {
			t.Errorf("%s must not be set by ChatCompletion", k)
		}
	}
}
