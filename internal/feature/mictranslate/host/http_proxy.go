package host

import (
	"bytes"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"
	"time"

	"translation-overlay/internal/feature/mictranslate/infra/languages"
	"translation-overlay/internal/platform/engine"
	"translation-overlay/internal/platform/timing"
)

var pipelineSecretFormKeys = map[string]struct{}{
	"forward_translate":           {},
	"english_asr_engine":          {},
	"openai_transcribe_model":     {},
	"openai_whisper_model":        {},
	"openrouter_transcribe_model": {},
	"transcribe_model":            {},
	"translate_model":             {},
	"openai_api_key":              {},
	"openai_base_url":             {},
	"openrouter_api_key":          {},
	"openrouter_base_url":         {},
	"openai_forward_model":        {},
	"openai_backtrans_model":      {},
	"pipeline_mode":               {},
	"api_provider":                {},
	"multimodal_model":            {},
}

var transcribeSecretFormKeys = map[string]struct{}{
	"asr_engine":                  {},
	"openai_transcribe_model":     {},
	"openai_whisper_model":        {},
	"openrouter_transcribe_model": {},
	"transcribe_model":            {},
	"openai_api_key":              {},
	"openai_base_url":             {},
	"openrouter_api_key":          {},
	"openrouter_base_url":         {},
	"api_provider":                {},
}

func (h *Host) injectPipelineSettings(mw *multipart.Writer) {
	s := h.readSettingsFromDisk()
	prof := languages.Get(s.TargetLanguage)
	_ = mw.WriteField("forward_translate", s.ForwardTranslator)
	_ = mw.WriteField("english_asr_engine", s.EnglishASREngine)
	_ = mw.WriteField("openai_transcribe_model", s.OpenAITranscribeModel)
	_ = mw.WriteField("openai_whisper_model", s.OpenAIWhisperModel)
	_ = mw.WriteField("openrouter_transcribe_model", s.OpenRouterTranscribeModel)
	_ = mw.WriteField("transcribe_model", s.TranscribeModel)
	_ = mw.WriteField("translate_model", s.TranslateModel)
	_ = mw.WriteField("speech_language", prof.SourceASRLang)
	_ = mw.WriteField("src_nllb", prof.SrcNLLB)
	_ = mw.WriteField("tgt_nllb", prof.TgtNLLB)
	_ = mw.WriteField("target_lang", prof.ID)
	_ = mw.WriteField("back_src_nllb", prof.BackSrcNLLB)
	if strings.TrimSpace(s.OpenAIAPIKey) != "" {
		_ = mw.WriteField("openai_api_key", s.OpenAIAPIKey)
	}
	if s.OpenAIBaseURL != "" {
		_ = mw.WriteField("openai_base_url", s.OpenAIBaseURL)
	}
	if strings.TrimSpace(s.OpenRouterAPIKey) != "" {
		_ = mw.WriteField("openrouter_api_key", s.OpenRouterAPIKey)
	}
	if s.OpenRouterBaseURL != "" {
		_ = mw.WriteField("openrouter_base_url", s.OpenRouterBaseURL)
	}
	_ = mw.WriteField("openai_forward_model", s.OpenAIForwardModel)
	_ = mw.WriteField("openai_backtrans_model", s.OpenAIBacktransModel)
	_ = mw.WriteField("pipeline_mode", s.PipelineMode)
	_ = mw.WriteField("api_provider", s.APIProvider)
	_ = mw.WriteField("multimodal_model", s.MultimodalModel)
}

func (h *Host) injectTranscribeSettings(mw *multipart.Writer) {
	s := h.readSettingsFromDisk()
	prof := languages.Get(s.TargetLanguage)
	asr := s.EnglishASREngine
	_ = mw.WriteField("asr_engine", asr)
	_ = mw.WriteField("language", prof.TargetASRLang)
	_ = mw.WriteField("api_provider", s.APIProvider)
	_ = mw.WriteField("transcribe_model", s.TranscribeModel)
	if asr == "openai" || asr == "openai_whisper" {
		_ = mw.WriteField("openai_transcribe_model", s.OpenAITranscribeModel)
		_ = mw.WriteField("openai_whisper_model", s.OpenAIWhisperModel)
		if strings.TrimSpace(s.OpenAIAPIKey) != "" {
			_ = mw.WriteField("openai_api_key", s.OpenAIAPIKey)
		}
		if s.OpenAIBaseURL != "" {
			_ = mw.WriteField("openai_base_url", s.OpenAIBaseURL)
		}
	}
	if asr == "openrouter" {
		_ = mw.WriteField("openrouter_transcribe_model", s.OpenRouterTranscribeModel)
		if strings.TrimSpace(s.OpenRouterAPIKey) != "" {
			_ = mw.WriteField("openrouter_api_key", s.OpenRouterAPIKey)
		}
		if s.OpenRouterBaseURL != "" {
			_ = mw.WriteField("openrouter_base_url", s.OpenRouterBaseURL)
		}
	}
}

func engineURL() string { return engine.EngineURL() }

var httpClient = &http.Client{Timeout: 3 * time.Minute}
var httpClientShort = &http.Client{Timeout: 15 * time.Second}

func (h *Host) forwardMultipart(w http.ResponseWriter, r *http.Request, enginePath string) {
	totalStart := time.Now()
	parseStart := time.Now()
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	parseMs := time.Since(parseStart)
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	buildStart := time.Now()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	keys := make([]string, 0, len(r.MultipartForm.Value))
	for k := range r.MultipartForm.Value {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, skip := pipelineSecretFormKeys[key]; skip && strings.HasSuffix(enginePath, "/pipeline") {
			continue
		}
		if _, skip := transcribeSecretFormKeys[key]; skip && strings.HasSuffix(enginePath, "/transcribe") {
			continue
		}
		for _, v := range r.MultipartForm.Value[key] {
			_ = mw.WriteField(key, v)
		}
	}
	if strings.HasSuffix(enginePath, "/pipeline") {
		h.injectPipelineSettings(mw)
	}
	if strings.HasSuffix(enginePath, "/transcribe") {
		h.injectTranscribeSettings(mw)
	}
	part, err := mw.CreateFormFile("file", header.Filename)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := io.Copy(part, file); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := mw.Close(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	buildMs := time.Since(buildStart)

	engineURLPath := enginePath
	if rq := strings.TrimSpace(r.URL.RawQuery); rq != "" {
		engineURLPath = enginePath + "?" + rq
	}
	req, err := http.NewRequest(http.MethodPost, engineURL()+engineURLPath, &buf)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	roundtripStart := time.Now()
	resp, err := httpClient.Do(req)
	roundtripMs := time.Since(roundtripStart)
	if err != nil {
		log.Printf("engine proxy POST %s: %v (roundtrip %s)", engineURL()+engineURLPath, err, roundtripMs.Round(time.Millisecond))
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	readStart := time.Now()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	readMs := time.Since(readStart)
	totalMs := time.Since(totalStart)
	if resp.StatusCode == http.StatusOK {
		body = timing.InjectGoTimings(body, map[string]float64{
			"go_parse_multipart_ms":  timing.DurationMS(parseMs),
			"go_build_body_ms":       timing.DurationMS(buildMs),
			"go_engine_roundtrip_ms": timing.DurationMS(roundtripMs),
			"go_read_response_ms":    timing.DurationMS(readMs),
			"go_proxy_total_ms":      timing.DurationMS(totalMs),
		})
	}
	if resp.StatusCode >= 400 {
		snip := strings.TrimSpace(string(body))
		if len(snip) > 240 {
			snip = snip[:240] + "…"
		}
		log.Printf(
			"engine proxy POST %s -> %d total=%s parse=%s build=%s roundtrip=%s read=%s %s",
			engineURL()+engineURLPath, resp.StatusCode,
			totalMs.Round(time.Millisecond), parseMs.Round(time.Millisecond), buildMs.Round(time.Millisecond),
			roundtripMs.Round(time.Millisecond), readMs.Round(time.Millisecond), snip,
		)
	} else {
		log.Printf(
			"engine proxy POST %s -> %d total=%s parse=%s build=%s roundtrip=%s read=%s resp=%d bytes",
			engineURL()+engineURLPath, resp.StatusCode,
			totalMs.Round(time.Millisecond), parseMs.Round(time.Millisecond), buildMs.Round(time.Millisecond),
			roundtripMs.Round(time.Millisecond), readMs.Round(time.Millisecond), len(body),
		)
		timing.LogEngineMeta("engine proxy")
	}
	for k, vv := range resp.Header {
		if strings.EqualFold(k, "Content-Type") && len(vv) > 0 {
			w.Header().Set("Content-Type", vv[0])
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

func proxyGET(w http.ResponseWriter, path string) {
	req, err := http.NewRequest(http.MethodGet, engineURL()+path, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (h *Host) proxyPlayWav(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hadDevice := false
	for k, vv := range r.MultipartForm.Value {
		for _, v := range vv {
			if k == "device_id" || k == "device_name" {
				hadDevice = true
			}
			_ = mw.WriteField(k, v)
		}
	}
	if !hadDevice {
		s := h.readSettingsFromDisk()
		if s.OutputDeviceName != "" {
			_ = mw.WriteField("device_name", s.OutputDeviceName)
		}
	}
	part, err := mw.CreateFormFile("file", header.Filename)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := io.Copy(part, file); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := mw.Close(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req, err := http.NewRequest(http.MethodPost, engineURL()+"/play-wav", &buf)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := httpClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		if strings.EqualFold(k, "Content-Type") && len(vv) > 0 {
			w.Header().Set("Content-Type", vv[0])
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
