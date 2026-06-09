package host

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type speakTTSInJSON struct {
	Text string `json:"text"`
}

func (h *Host) proxySpeakTTS(w http.ResponseWriter, r *http.Request) {
	const maxBody = 32 << 10
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(body) > maxBody {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	var in speakTTSInJSON
	if err := json.Unmarshal(body, &in); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		http.Error(w, "empty text", http.StatusBadRequest)
		return
	}
	s := h.readSettingsFromDisk()
	fwd := map[string]any{
		"text":         text,
		"tts_engine":   s.TTSEngine,
		"model":        s.OpenAITTSModel,
		"voice":        s.TTSVoiceName,
		"instructions": s.OpenAITTSInstructions,
		"device_name":  s.OutputDeviceName,
	}
	if strings.TrimSpace(s.OpenAIAPIKey) != "" {
		fwd["openai_api_key"] = s.OpenAIAPIKey
	}
	if s.OpenAIBaseURL != "" {
		fwd["openai_base_url"] = s.OpenAIBaseURL
	}
	if strings.TrimSpace(s.OpenRouterAPIKey) != "" {
		fwd["openrouter_api_key"] = s.OpenRouterAPIKey
	}
	if s.OpenRouterBaseURL != "" {
		fwd["openrouter_base_url"] = s.OpenRouterBaseURL
	}
	bout, err := json.Marshal(fwd)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, engineURL()+"/speak-tts", bytes.NewReader(bout))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	for k, vv := range resp.Header {
		if strings.EqualFold(k, "Content-Type") && len(vv) > 0 {
			w.Header().Set("Content-Type", vv[0])
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}
