package cloudapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestResolveDashScope(t *testing.T) {
	key, base, provider, err := Credentials{APIProvider: "dashscope", DashScopeKey: "k"}.resolve()
	if err != nil {
		t.Fatal(err)
	}
	if provider != "dashscope" || key != "k" || base != dashScopeDefaultBase {
		t.Fatalf("resolve = key:%q base:%q provider:%q", key, base, provider)
	}
}

func TestDashScopeTranscribeWAV(t *testing.T) {
	var gotPath string
	gotBody := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"これはテストです"}}]}`)
	}))
	defer srv.Close()

	creds := Credentials{APIProvider: "dashscope", DashScopeKey: "k", DashScopeBase: srv.URL}
	text, detected, err := DashScopeTranscribeWAV(creds, "qwen3-asr-flash", "ja", "proper nouns: Penguin", make([]byte, 1000), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if text != "これはテストです" {
		t.Errorf("text = %q", text)
	}
	if detected == nil || *detected != "ja" {
		t.Errorf("detected = %v, want ja", detected)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("want leading context system msg + user msg, got %d", len(msgs))
	}
	if m0, _ := msgs[0].(map[string]any); m0["role"] != "system" {
		t.Errorf("first message role = %v, want system (context)", m0["role"])
	}
	if raw, _ := json.Marshal(gotBody); !strings.Contains(string(raw), "data:audio/wav;base64,") {
		t.Error("request missing data-URI input_audio part")
	}
}

func TestDeepgramTranscribeWAV(t *testing.T) {
	var gotPath, gotQuery, gotAuth, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		_, _ = io.WriteString(w, `{"results":{"channels":[{"detected_language":"ja","alternatives":[{"transcript":"これはテストです"}]}]}}`)
	}))
	defer srv.Close()

	creds := Credentials{DeepgramKey: "dgk", DeepgramBase: srv.URL}
	text, detected, err := DeepgramTranscribeWAV(creds, "nova-2", "ja", make([]byte, 1000), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if text != "これはテストです" {
		t.Errorf("text = %q", text)
	}
	if detected == nil || *detected != "ja" {
		t.Errorf("detected = %v, want ja", detected)
	}
	if gotPath != "/v1/listen" {
		t.Errorf("path = %q, want /v1/listen", gotPath)
	}
	if gotAuth != "Token dgk" {
		t.Errorf("auth = %q, want \"Token dgk\"", gotAuth)
	}
	if gotCT != "audio/wav" {
		t.Errorf("content-type = %q, want audio/wav", gotCT)
	}
	for _, want := range []string{"model=nova-2", "language=ja", "smart_format=true"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
}

func TestCloudTranscribeDeepgram(t *testing.T) {
	var path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		auth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"results":{"channels":[{"alternatives":[{"transcript":"これはマイクのテストです"}]}]}}`)
	}))
	defer srv.Close()

	creds := Credentials{DeepgramKey: "dgk", DeepgramBase: srv.URL}
	text, _, err := CloudTranscribe(creds, "deepgram", "nova-2", "ja", make([]byte, 1000))
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/listen" {
		t.Errorf("CloudTranscribe(deepgram) hit %q, want /v1/listen", path)
	}
	if auth != "Token dgk" {
		t.Errorf("auth = %q, want \"Token dgk\"", auth)
	}
	if text != "これはマイクのテストです" {
		t.Errorf("text = %q", text)
	}
}

func TestDeepgramTranscribeWAVAutoDetect(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"results":{"channels":[{"detected_language":"zh-CN","alternatives":[{"transcript":"你好"}]}]}}`)
	}))
	defer srv.Close()

	creds := Credentials{DeepgramKey: "dgk", DeepgramBase: srv.URL}
	text, detected, err := DeepgramTranscribeWAV(creds, "nova-2", "", make([]byte, 1000), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if text != "你好" {
		t.Errorf("text = %q", text)
	}
	if detected == nil || *detected != "zh" {
		t.Errorf("detected = %v, want zh (truncated from zh-CN)", detected)
	}
	q, _ := url.ParseQuery(gotQuery)
	if q.Get("detect_language") != "true" {
		t.Errorf("query %q missing detect_language=true for empty language", gotQuery)
	}
	if q.Has("language") {
		t.Errorf("query %q should not send a language param when auto-detecting", gotQuery)
	}
}
