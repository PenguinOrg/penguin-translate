package cloudapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
