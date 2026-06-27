//go:build smoke

package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var appExe string

func TestMain(m *testing.M) {
	exe, cleanup, err := resolveExe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "smoke: cannot obtain app binary:", err)
		os.Exit(1)
	}
	appExe = exe
	code := m.Run()
	if cleanup != nil {
		cleanup()
	}
	os.Exit(code)
}

func resolveExe() (string, func(), error) {
	if p := strings.TrimSpace(os.Getenv("TO_SMOKE_EXE")); p != "" {
		if !filepath.IsAbs(p) {
			p = filepath.Join(repoRoot(), p)
		}
		if _, err := os.Stat(p); err != nil {
			return "", nil, fmt.Errorf("TO_SMOKE_EXE %q: %w", p, err)
		}
		return p, nil, nil
	}

	if release := filepath.Join(repoRoot(), "build", "penguin-translate.exe"); fileExists(release) {
		return release, nil, nil
	}

	tmp, err := os.MkdirTemp("", "smoke-exe-")
	if err != nil {
		return "", nil, err
	}
	out := filepath.Join(tmp, "penguin-translate.exe")
	cmd := exec.Command("go", "build", "-tags", "overlay_embedded", "-o", out, "./cmd/app")
	cmd.Dir = repoRoot()
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if buildOut, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(tmp)
		return "", nil, fmt.Errorf("go build ./cmd/app: %v\n%s", err, buildOut)
	}
	return out, func() { os.RemoveAll(tmp) }, nil
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func repoRoot() string {
	abs, _ := filepath.Abs(filepath.Join("..", ".."))
	return abs
}

type app struct {
	baseURL string
	cmd     *exec.Cmd
	logs    *syncBuf
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func launch(t *testing.T) *app {
	t.Helper()
	port := freePort(t)
	dataDir := t.TempDir()

	logs := &syncBuf{}
	cmd := exec.Command(appExe, "-http", fmt.Sprintf(":%d", port))
	cmd.Dir = repoRoot()
	cmd.Env = append(os.Environ(), "TO_DATA_DIR="+dataDir)
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start app: %v", err)
	}

	a := &app{baseURL: fmt.Sprintf("http://127.0.0.1:%d", port), cmd: cmd, logs: logs}
	if err := waitHealthy(a.baseURL, 30*time.Second); err != nil {
		t.Logf("app log:\n%s", logs.String())
		killTree(cmd)
		t.Fatalf("app did not become healthy: %v", err)
	}
	return a
}

func teardown(t *testing.T, a *app) {
	t.Helper()
	killTree(a.cmd)
	_ = a.cmd.Wait()

	if running, names := overlayProcessesRunning(); running {
		t.Errorf("orphaned overlay process(es) after shutdown: %v", names)
	}
	if log := a.logs.String(); strings.Contains(log, "panic:") || strings.Contains(strings.ToLower(log), "panic(") {
		t.Errorf("panic in app log:\n%s", tail(log, 2000))
	}
}

type renderProbe struct {
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	OpaquePx  int64  `json:"opaque_px"`
	InkPx     int64  `json:"ink_px"`
	PixelHash string `json:"pixel_hash"`
}

func runRenderProbe(t *testing.T, overlayExe, outPNG, text string) renderProbe {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, overlayExe, "--render-probe", outPNG, text).Output()
	if err != nil {
		t.Fatalf("render-probe %q: %v (stdout=%s)", text, err, out)
	}
	var pr renderProbe
	if err := json.Unmarshal(bytes.TrimSpace(out), &pr); err != nil {
		t.Fatalf("parse render-probe manifest %q: %v", out, err)
	}
	return pr
}

func TestSmoke(t *testing.T) {
	a := launch(t)
	defer teardown(t, a)

	t.Run("health", func(t *testing.T) {
		var body map[string]any
		getJSON(t, a, "/health", &body)
		if body["ui"] != "ok" {
			t.Fatalf("health ui != ok: %v", body)
		}
	})

	t.Run("languages", func(t *testing.T) {
		var raw json.RawMessage
		getJSON(t, a, "/api/languages", &raw)
		if len(raw) < 3 {
			t.Fatalf("languages looks empty: %s", raw)
		}
	})

	t.Run("caption_presets", func(t *testing.T) {
		mustGet200(t, a, "/api/caption-presets")
	})

	t.Run("loopback_devices", func(t *testing.T) {
		mustGet200(t, a, "/api/loopback/devices")
	})

	t.Run("cuda_devices", func(t *testing.T) {
		mustGet200(t, a, "/api/cuda-devices")
	})

	t.Run("plugins", func(t *testing.T) {
		mustGet200(t, a, "/api/plugins")
	})

	t.Run("settings_roundtrip", func(t *testing.T) {
		const marker = "SMOKE_MARKER"
		var before map[string]any
		getJSON(t, a, "/api/settings", &before)

		postJSON(t, a, "/api/settings", map[string]any{"skip_words": []string{marker}}, nil)

		var after map[string]any
		getJSON(t, a, "/api/settings", &after)
		words, _ := after["skip_words"].([]any)
		if !containsStr(words, marker) {
			t.Fatalf("skip_words did not persist marker: %v", after["skip_words"])
		}
		postJSON(t, a, "/api/settings", map[string]any{"skip_words": before["skip_words"]}, nil)
	})

	t.Run("health_summary", func(t *testing.T) {
		var s struct {
			Engine struct{ State, Detail string } `json:"engine"`
			Models struct{ State, Detail string } `json:"models"`
			OpenAI struct{ State, Detail string } `json:"openai"`
		}
		getJSON(t, a, "/api/health-summary", &s)
		t.Logf("engine=%s(%s) models=%s openai=%s", s.Engine.State, s.Engine.Detail, s.Models.State, s.OpenAI.State)
	})

	t.Run("desktop_overlay_lifecycle", func(t *testing.T) {
		postJSON(t, a, "/api/desktop-overlay/start", map[string]any{}, nil)
		var st map[string]any
		deadline := time.Now().Add(5 * time.Second)
		for {
			getJSON(t, a, "/api/desktop-overlay/status", &st)
			if running, _ := st["running"].(bool); running || time.Now().After(deadline) {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if running, _ := st["running"].(bool); !running {
			le, _ := st["last_error"].(string)
			detail, _ := st["detail"].(string)
			switch {
			case strings.Contains(le, "overlay binary missing"):
				t.Skipf("overlay not embedded in this binary (%s) — build with build.ps1 or -tags overlay_embedded", le)
			case os.Getenv("SMOKE_CI") != "":
				t.Skipf("overlay did not reach running on headless CI (detail=%q last_error=%q)", detail, le)
			default:
				t.Fatalf("desktop overlay did not report running: %v", st)
			}
		}
		postJSON(t, a, "/api/desktop-overlay/stop", map[string]any{}, nil)
	})

	t.Run("vr_overlay_status", func(t *testing.T) {
		var st map[string]any
		getJSON(t, a, "/api/overlay/status", &st)
		if has, _ := st["has_steamvr"].(bool); !has {
			t.Skipf("no SteamVR runtime (status: %v) — endpoint OK, VR path not exercised", st["detail"])
		}
	})

	t.Run("overlay_render_probe", func(t *testing.T) {
		overlayExe := strings.TrimSpace(os.Getenv("TO_OVERLAY_EXE"))
		if overlayExe == "" || !fileExists(overlayExe) {
			t.Skipf("TO_OVERLAY_EXE not set to an existing overlay binary (%q) — build runtime/overlay and set it to exercise pixel rendering", overlayExe)
		}
		dir := t.TempDir()
		outDir := dir
		if ad := strings.TrimSpace(os.Getenv("E2E_ARTIFACT_DIR")); ad != "" {
			if !filepath.IsAbs(ad) {
				ad = filepath.Join(repoRoot(), ad)
			}
			if err := os.MkdirAll(ad, 0o755); err == nil {
				outDir = ad
			}
		}
		probePNG := filepath.Join(outDir, "overlay-render-probe.png")
		p1 := runRenderProbe(t, overlayExe, probePNG, "this is an audio capture test")
		if p1.Width <= 0 || p1.Height <= 4 {
			t.Fatalf("render probe produced a degenerate strip: %+v", p1)
		}
		if p1.InkPx < 100 {
			t.Fatalf("render probe drew too few ink pixels (%d) — caption text did not render: %+v", p1.InkPx, p1)
		}
		if fi, err := os.Stat(probePNG); err != nil || fi.Size() < 100 {
			t.Fatalf("render probe PNG missing or too small: %v", err)
		}
		p2 := runRenderProbe(t, overlayExe, filepath.Join(dir, "b.png"), "a completely different line of text")
		if p2.InkPx < 100 {
			t.Fatalf("second render probe drew too few ink pixels (%d): %+v", p2.InkPx, p2)
		}
		if p1.PixelHash == p2.PixelHash {
			t.Fatalf("different captions produced identical pixels (hash %s) — render is not content-sensitive", p1.PixelHash)
		}
		t.Logf("overlay render probe OK: %dx%d ink=%d (content-sensitive)", p1.Width, p1.Height, p1.InkPx)
	})

	t.Run("overlay_vr_probe", func(t *testing.T) {
		overlayExe := strings.TrimSpace(os.Getenv("TO_OVERLAY_EXE"))
		fakeDLL := strings.TrimSpace(os.Getenv("TO_FAKE_OPENVR_DLL"))
		if overlayExe == "" || !fileExists(overlayExe) {
			t.Skipf("TO_OVERLAY_EXE not set to an existing overlay binary (%q)", overlayExe)
		}
		if fakeDLL == "" || !fileExists(fakeDLL) {
			t.Skipf("TO_FAKE_OPENVR_DLL not set to a built fake openvr_api.dll (%q) — build runtime/overlay/fake-openvr", fakeDLL)
		}
		dir := t.TempDir()
		raw, err := os.ReadFile(fakeDLL)
		if err != nil {
			t.Fatalf("read fake dll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "openvr_api.dll"), raw, 0o755); err != nil {
			t.Fatalf("place fake dll: %v", err)
		}
		recPath := filepath.Join(dir, "vr-rec.json")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, overlayExe, "--vr-probe")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "WT_FAKE_VR_OUT="+recPath)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("vr-probe run: %v (stdout=%s)", err, out)
		}

		var pm struct {
			VrReady   bool `json:"vr_ready"`
			PresentOK bool `json:"present_ok"`
			SubmitW   int  `json:"submit_w"`
			SubmitH   int  `json:"submit_h"`
		}
		if err := json.Unmarshal(bytes.TrimSpace(out), &pm); err != nil {
			t.Fatalf("parse vr-probe manifest %q: %v", out, err)
		}
		if !pm.VrReady {
			t.Fatalf("overlay did not reach vr_ready against the fake runtime: %s", out)
		}
		if !pm.PresentOK || pm.SubmitW <= 0 || pm.SubmitH <= 0 {
			t.Fatalf("overlay VR present failed or degenerate: %+v", pm)
		}

		recRaw, err := os.ReadFile(recPath)
		if err != nil {
			t.Fatalf("read fake record (%s): %v", recPath, err)
		}
		var rec struct {
			SetRawW      int    `json:"set_raw_w"`
			SetRawH      int    `json:"set_raw_h"`
			BPP          int    `json:"bpp"`
			ByteSum      uint64 `json:"byte_sum"`
			ShowCount    int    `json:"show_count"`
			TransformSet int    `json:"transform_set"`
			CreateCount  int    `json:"create_count"`
		}
		if err := json.Unmarshal(bytes.TrimSpace(recRaw), &rec); err != nil {
			t.Fatalf("parse fake record %q: %v", recRaw, err)
		}
		if rec.CreateCount < 1 {
			t.Errorf("overlay never created a VR overlay: %+v", rec)
		}
		if rec.ShowCount < 1 {
			t.Errorf("overlay never showed the VR overlay: %+v", rec)
		}
		if rec.TransformSet != 1 {
			t.Errorf("overlay never set the VR transform: %+v", rec)
		}
		if rec.BPP != 4 {
			t.Errorf("VR frame bpp = %d, want 4 (premultiplied RGBA)", rec.BPP)
		}
		if rec.SetRawW != pm.SubmitW || rec.SetRawH != pm.SubmitH {
			t.Errorf("VR boundary received %dx%d but overlay rendered %dx%d",
				rec.SetRawW, rec.SetRawH, pm.SubmitW, pm.SubmitH)
		}
		if rec.ByteSum == 0 {
			t.Errorf("VR frame reached the boundary blank (byte_sum 0)")
		}
		t.Logf("VR probe OK: %dx%d bpp=%d submitted to the (fake) OpenVR boundary; create=%d show=%d transform=%d",
			rec.SetRawW, rec.SetRawH, rec.BPP, rec.CreateCount, rec.ShowCount, rec.TransformSet)
	})

	t.Run("loopback_ws", func(t *testing.T) {
		ws, _, err := websocket.DefaultDialer.Dial(wsURL(a, "/api/loopback"), nil)
		if err != nil {
			t.Fatalf("dial /api/loopback: %v", err)
		}
		defer ws.Close()
		if err := ws.WriteJSON(map[string]string{"cmd": "start"}); err != nil {
			t.Fatalf("ws start: %v", err)
		}
		ws.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, msg, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("no response to loopback start: %v", err)
		}
		_ = ws.WriteJSON(map[string]string{"cmd": "stop"})
		if bytes.Contains(msg, []byte(`"error"`)) {
			t.Skipf("loopback capture unavailable on this machine: %s — endpoint OK", msg)
		}
	})

	t.Run("vrchat_osc_send", func(t *testing.T) {
		code := postJSON(t, a, "/api/plugins/vrchat-osc/send", map[string]string{"text": "smoke"}, nil)
		if code == http.StatusBadGateway {
			t.Skipf("osc emit returned 502 (no socket) — endpoint OK")
		}
		if code != http.StatusOK {
			t.Fatalf("osc send status %d", code)
		}
	})

	if runtime.GOOS == "windows" {
		t.Run("windows_list", func(t *testing.T) {
			resp, err := httpClient.Get(a.baseURL + "/api/windows")
			if err != nil {
				t.Fatalf("GET /api/windows: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound {
				t.Skip("window-OCR feature not mounted (OneOCR runtime/data absent on this machine)")
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET /api/windows: status %d", resp.StatusCode)
			}
		})
	}

	mock := startMockCloud(t)
	postJSON(t, a, "/api/settings", map[string]any{
		"openai_api_key":      "sk-test",
		"openai_base_url":     mock.baseURL(),
		"openrouter_api_key":  "sk-test",
		"openrouter_base_url": mock.baseURL(),
		"practice":            map[string]any{"forward_translator": "openai", "api_provider": "openai"},
	}, nil)

	t.Run("translate_text", func(t *testing.T) {
		var out map[string]any
		code := postJSON(t, a, "/api/translate-text", map[string]string{"text": "hello world"}, &out)
		if code != http.StatusOK {
			t.Fatalf("translate-text status %d: %v", code, out)
		}
		if !anyNonEmptyString(out) {
			t.Fatalf("translate-text returned no text: %v", out)
		}
		if atomic.LoadInt32(&mock.chatHits) == 0 {
			t.Fatalf("app did not call the mock translate endpoint (routing/forward_translator wrong)")
		}
	})

	t.Run("transcribe", func(t *testing.T) {
		code, body := postMultipartWAV(t, a, "/api/transcribe", makeTestWAV())
		if code != http.StatusOK {
			t.Fatalf("transcribe status %d: %s", code, tail(body, 500))
		}
		if atomic.LoadInt32(&mock.sttHits) == 0 {
			t.Fatalf("app did not call the mock transcribe endpoint")
		}
	})

	t.Run("translate_multi", func(t *testing.T) {
		var out struct {
			Results []map[string]any `json:"results"`
		}
		code := postJSON(t, a, "/api/translate-text", map[string]any{
			"text":             "good morning",
			"source_language":  "en",
			"target_languages": []string{"ja", "zh", "ko"},
		}, &out)
		if code != http.StatusOK {
			t.Fatalf("translate-text multi status %d", code)
		}
		if len(out.Results) != 3 {
			t.Fatalf("want 3 fanned-out results, got %d: %v", len(out.Results), out.Results)
		}
		for _, row := range out.Results {
			if row["language"] == "ja" {
				if toks, _ := row["reading_aid_tokens"].([]any); len(toks) == 0 {
					t.Errorf("ja row missing furigana reading_aid_tokens: %v", row)
				}
			}
		}
	})

	t.Run("transcribe_segment", func(t *testing.T) {
		postJSON(t, a, "/api/settings", map[string]any{
			"openai_api_key":  "sk-test",
			"openai_base_url": mock.baseURL(),
			"audio": map[string]any{
				"api_provider":     "openai",
				"pipeline_mode":    "split",
				"primary_language": "ja",
				"transcribe_model": "mock-asr",
				"translate_model":  "mock-tr",
			},
		}, nil)
		before := atomic.LoadInt32(&mock.batchHits)
		code, body := postTranscribeSegment(t, a, makeTestWAV(), "ja")
		if code != http.StatusOK {
			t.Fatalf("transcribe-segment status %d: %s", code, tail(body, 500))
		}
		if !strings.Contains(body, `"segments"`) {
			t.Fatalf("transcribe-segment returned no segments: %s", tail(body, 500))
		}
		if atomic.LoadInt32(&mock.batchHits) == before {
			t.Fatalf("translate_to_en did not reach the mock batch endpoint: %s", tail(body, 300))
		}
	})

	t.Run("audio_runtime", func(t *testing.T) { mustGet200(t, a, "/api/audio/runtime") })
	t.Run("engine_health", func(t *testing.T) { mustGet200(t, a, "/api/engine-health") })
	t.Run("devices_output", func(t *testing.T) { mustGet200(t, a, "/api/devices/output") })
}

type mockCloud struct {
	srv       *httptest.Server
	chatHits  int32
	sttHits   int32
	batchHits int32
}

func startMockCloud(t *testing.T) *mockCloud {
	t.Helper()
	m := &mockCloud{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&m.chatHits, 1)
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		w.Header().Set("Content-Type", "application/json")
		switch classifyChat(body) {
		case chatMultimodal:
			_, _ = w.Write(chatReplyJSON(`{"text":"これはテストです","english":"this is a test","detected_lang":"ja"}`))
		case chatBatch:
			atomic.AddInt32(&m.batchHits, 1)
			_, _ = w.Write(chatReplyJSON(mockBatchArray(body)))
		default:
			_, _ = w.Write(chatReplyJSON("こんにちは世界"))
		}
	})
	mux.HandleFunc("/v1/audio/transcriptions", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&m.sttHits, 1)
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(mockField(body, "language"), "ja") {
			io.WriteString(w, `{"text":"これはおんせいのテストです","language":"ja"}`)
			return
		}
		io.WriteString(w, `{"text":"hello world","language":"en"}`)
	})
	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockCloud) baseURL() string { return m.srv.URL + "/v1" }

func chatReplyJSON(content string) []byte {
	b, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{{"message": map[string]any{"content": content}}},
	})
	return b
}

type chatKind int

const (
	chatForward chatKind = iota
	chatBatch
	chatMultimodal
)

func classifyChat(body []byte) chatKind {
	var req struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &req) != nil {
		return chatForward
	}
	for _, msg := range req.Messages {
		var parts []struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(msg.Content, &parts) == nil {
			for _, p := range parts {
				if p.Type == "input_audio" {
					return chatMultimodal
				}
			}
			continue
		}
		var s string
		if json.Unmarshal(msg.Content, &s) == nil && len(batchItems(s)) > 0 {
			return chatBatch
		}
	}
	return chatForward
}

func batchItems(s string) []int {
	var items []struct {
		I int `json:"i"`
	}
	if json.Unmarshal([]byte(s), &items) != nil || len(items) == 0 {
		return nil
	}
	idx := make([]int, len(items))
	for i, it := range items {
		idx[i] = it.I
	}
	return idx
}

func mockBatchArray(body []byte) string {
	n := 1
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &req) == nil {
		for _, msg := range req.Messages {
			if msg.Role == "user" {
				if items := batchItems(msg.Content); len(items) > 0 {
					n = len(items)
				}
			}
		}
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf(`{"i":%d,"en":"captured line"}`, i)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func mockField(body []byte, key string) string {
	s := string(body)
	if m := regexp.MustCompile(`"` + key + `"\s*:\s*"([^"]+)"`).FindStringSubmatch(s); m != nil {
		return m[1]
	}
	if m := regexp.MustCompile(`name="` + key + `"\r?\n\r?\n([^\r\n]+)`).FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

func makeTestWAV() []byte {
	const sr, secs = 16000, 0.5
	n := int(sr * secs)
	var body bytes.Buffer
	for i := 0; i < n; i++ {
		v := int16(2000 * (i % 64) / 64)
		body.WriteByte(byte(v))
		body.WriteByte(byte(v >> 8))
	}
	data := body.Bytes()
	var w bytes.Buffer
	w.WriteString("RIFF")
	writeU32(&w, uint32(36+len(data)))
	w.WriteString("WAVE")
	w.WriteString("fmt ")
	writeU32(&w, 16)
	writeU16(&w, 1)
	writeU16(&w, 1)
	writeU32(&w, sr)
	writeU32(&w, sr*2)
	writeU16(&w, 2)
	writeU16(&w, 16)
	w.WriteString("data")
	writeU32(&w, uint32(len(data)))
	w.Write(data)
	return w.Bytes()
}

func writeU32(w *bytes.Buffer, v uint32) {
	w.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
}
func writeU16(w *bytes.Buffer, v uint16) { w.Write([]byte{byte(v), byte(v >> 8)}) }

var httpClient = &http.Client{Timeout: 30 * time.Second}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitHealthy(base string, d time.Duration) error {
	deadline := time.Now().Add(d)
	var last error
	for time.Now().Before(deadline) {
		resp, err := httpClient.Get(base + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			last = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			last = err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return last
}

func getJSON(t *testing.T, a *app, path string, out any) {
	t.Helper()
	resp, err := httpClient.Get(a.baseURL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s: status %d: %s", path, resp.StatusCode, tail(string(b), 300))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("GET %s: decode: %v", path, err)
		}
	}
}

func mustGet200(t *testing.T, a *app, path string) {
	t.Helper()
	resp, err := httpClient.Get(a.baseURL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", path, resp.StatusCode)
	}
}

func postJSON(t *testing.T, a *app, path string, payload any, out any) int {
	t.Helper()
	body, _ := json.Marshal(payload)
	resp, err := httpClient.Post(a.baseURL+path, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func postMultipartWAV(t *testing.T, a *app, path string, wav []byte) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("language", "en")
	fw, _ := mw.CreateFormFile("file", "clip.wav")
	_, _ = fw.Write(wav)
	mw.Close()

	req, _ := http.NewRequest(http.MethodPost, a.baseURL+path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func postTranscribeSegment(t *testing.T, a *app, wav []byte, lang string) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("language", lang)
	_ = mw.WriteField("translate_to_en", "1")
	fw, _ := mw.CreateFormFile("file", "clip.wav")
	_, _ = fw.Write(wav)
	mw.Close()

	req, _ := http.NewRequest(http.MethodPost, a.baseURL+"/api/transcribe-segment", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/transcribe-segment: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func wsURL(a *app, path string) string {
	return "ws" + strings.TrimPrefix(a.baseURL, "http") + path
}

func containsStr(arr []any, s string) bool {
	for _, v := range arr {
		if str, ok := v.(string); ok && str == s {
			return true
		}
	}
	return false
}

func anyNonEmptyString(m map[string]any) bool {
	for _, v := range m {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprintf("%d", cmd.Process.Pid)).Run()
		return
	}
	_ = cmd.Process.Kill()
}

func overlayProcessesRunning() (bool, []string) {
	if runtime.GOOS != "windows" {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tasklist", "/FI", "IMAGENAME eq penguin-translate-overlay.exe", "/NH").Output()
	if err != nil {
		return false, nil
	}
	if strings.Contains(string(out), "penguin-translate-overlay.exe") {
		return true, []string{"penguin-translate-overlay.exe"}
	}
	return false, nil
}
