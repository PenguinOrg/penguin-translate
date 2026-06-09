package translate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"translation-overlay/internal/feature/mictranslate/infra/languages"
)

func nllbTargetCode(target string) string {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" || target == "en" {
		return "eng_Latn"
	}
	if l, ok := languages.Lang(target); ok && l.NLLBCode != "" {
		return l.NLLBCode
	}
	return "eng_Latn"
}

type NLLBClient struct {
	BaseURL string
	HTTP    *http.Client
	Target  string
}

func NewNLLB(baseURL string) *NLLBClient {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8744"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &NLLBClient{
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: 120 * time.Second},
	}
}

func CheckEngine(baseURL string) error {
	c := NewNLLB(baseURL)
	url := c.BaseURL + "/health"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach Penguin Translate engine at %s: %w — start Penguin Translate first", c.BaseURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Penguin Translate engine at %s returned %s from GET /health", c.BaseURL, resp.Status)
	}

	probe, err := http.NewRequest(http.MethodPost, c.BaseURL+"/translate", bytes.NewReader([]byte(`{"items":[],"source_lang":"auto"}`)))
	if err != nil {
		return err
	}
	probe.Header.Set("Content-Type", "application/json")
	resp, err = c.HTTP.Do(probe)
	if err != nil {
		return fmt.Errorf("cannot probe POST %s/translate: %w", c.BaseURL, err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf(
			"Penguin Translate engine at %s is running but POST /translate is missing (404). "+
				"Restart Penguin Translate so it syncs the latest engine bundle — or rebuild Penguin Translate if you run from source",
			c.BaseURL,
		)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Penguin Translate engine probe POST /translate: %s: %s", resp.Status, truncate(string(b), 200))
	}
	return nil
}

func (c *NLLBClient) ToTargetLine(text, sourceLang string) (LineResult, error) {
	batch, err := c.ToTargetBatch([]string{text}, sourceLang)
	if err != nil {
		return LineResult{}, err
	}
	if len(batch) == 0 {
		return LineResult{}, nil
	}
	return batch[0], nil
}

func (c *NLLBClient) ToTargetBatch(lines []string, sourceLang string) ([]LineResult, error) {
	if len(lines) == 0 {
		return nil, nil
	}
	items := make([]batchLineIn, len(lines))
	for i, line := range lines {
		items[i] = batchLineIn{ID: i, Text: line}
	}
	body := map[string]any{
		"items":       items,
		"source_lang": sourceLang,
		"target_lang": nllbTargetCode(c.Target),
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	url := c.BaseURL + "/translate"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Penguin Translate engine at %s: %w (is it running?)", c.BaseURL, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, formatEngineHTTPError(c.BaseURL, url, resp.StatusCode, b)
	}
	var parsed struct {
		Items []batchLineOut `json:"items"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return nil, fmt.Errorf("parse Penguin Translate response from %s: %w (body: %s)", url, err, truncate(string(b), 200))
	}
	out := make([]LineResult, len(lines))
	for _, item := range parsed.Items {
		if item.ID < 0 || item.ID >= len(out) {
			continue
		}
		out[item.ID] = LineResult{En: strings.TrimSpace(item.En), Roman: strings.TrimSpace(item.Roman)}
	}
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.TrimSpace(out[i].En) == "" {
			return nil, fmt.Errorf("Penguin Translate returned no translation for line %d (sent %d lines to %s)", i, len(lines), url)
		}
	}
	return out, nil
}

func formatEngineHTTPError(baseURL, url string, code int, body []byte) error {
	detail := parseFastAPIDetail(body)
	switch code {
	case http.StatusNotFound:
		return fmt.Errorf(
			"POST %s not found (404) — Penguin Translate engine at %s is outdated. "+
				"Quit and restart Penguin Translate (it re-syncs server.py on startup). %s",
			url, baseURL, detail,
		)
	case http.StatusServiceUnavailable:
		return fmt.Errorf("Penguin Translate engine at %s not ready (503): %s — wait for models to load or POST /load", baseURL, detail)
	default:
		if detail != "" {
			return fmt.Errorf("Penguin Translate %s POST %s: %s", http.StatusText(code), url, detail)
		}
		return fmt.Errorf("Penguin Translate %s POST %s: %s", http.StatusText(code), url, truncate(string(body), 400))
	}
}

func parseFastAPIDetail(body []byte) string {
	var payload struct {
		Detail any `json:"detail"`
	}
	if json.Unmarshal(body, &payload) != nil {
		s := strings.TrimSpace(string(body))
		if s == "" || s == `{"detail":"Not Found"}` {
			return ""
		}
		return truncate(s, 300)
	}
	switch d := payload.Detail.(type) {
	case string:
		return strings.TrimSpace(d)
	case []any:
		var parts []string
		for _, item := range d {
			if m, ok := item.(map[string]any); ok {
				if msg, ok := m["msg"].(string); ok {
					parts = append(parts, msg)
				}
			}
		}
		return strings.Join(parts, "; ")
	default:
		return ""
	}
}
