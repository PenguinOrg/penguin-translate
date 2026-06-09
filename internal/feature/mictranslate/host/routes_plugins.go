package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"translation-overlay/internal/feature/mictranslate/infra/languages"
	"translation-overlay/internal/feature/mictranslate/infra/plugin"
	"translation-overlay/internal/feature/mictranslate/infra/plugin/vrchatosc"
	scorepkg "translation-overlay/internal/feature/mictranslate/infra/score"
	"translation-overlay/internal/feature/mictranslate/nativecloud"
	"translation-overlay/internal/platform/timing"
)

func handleLanguages(w http.ResponseWriter, r *http.Request) {
	if !isGetOrHead(r) {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"languages": languages.All(),
		"catalog":   languages.Catalog(),
	})
}

func handlePluginsList(w http.ResponseWriter, r *http.Request) {
	if !isGetOrHead(r) {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"plugins": plugin.Default.List(),
		"configs": plugin.Default.PublicConfigs(),
	})
}

type vrchatOscSendJSON struct {
	Text string `json:"text"`
}

func handleVRChatOscSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	const max = 16 << 10
	body, err := io.ReadAll(io.LimitReader(r.Body, max+1))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(body) > max {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	var in vrchatOscSendJSON
	if err := json.Unmarshal(body, &in); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		http.Error(w, "empty text", http.StatusBadRequest)
		return
	}
	pub := plugin.Default.PublicConfigs()["vrchat_osc"]
	cfg := vrchatosc.ConfigFromPublic(pub)
	if err := vrchatosc.SendManual(cfg, text); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
}

func (h *Host) dispatchTranslationPlugins(r *http.Request, payload map[string]any) {
	s := h.readSettingsFromDisk()
	prof := languages.Get(s.TargetLanguage)
	target, _ := payload["japanese"].(string)
	if target == "" {
		if t, ok := payload["target"].(string); ok {
			target = t
		}
	}
	english, _ := payload["english"].(string)
	back, _ := payload["back_english"].(string)
	var furi []plugin.FuriganaToken
	if raw, ok := payload["furigana"].([]any); ok && prof.HasFurigana {
		for _, item := range raw {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			ft := plugin.FuriganaToken{}
			if v, ok := m["surface"].(string); ok {
				ft.Surface = v
			}
			if v, ok := m["reading"].(string); ok {
				ft.Reading = v
			}
			furi = append(furi, ft)
		}
	}
	plugin.Default.Dispatch(r.Context(), plugin.Event{
		Type: plugin.EventTranslationReady,
		Translation: &plugin.TranslationPayload{
			TargetLang:  prof.ID,
			English:     english,
			Target:      target,
			BackEnglish: back,
			Furigana:    furi,
		},
	})
}

func (h *Host) dispatchPracticePassed(r *http.Request, english, target, spoken string, score float64) {
	s := h.readSettingsFromDisk()
	prof := languages.Get(s.TargetLanguage)
	plugin.Default.Dispatch(r.Context(), plugin.Event{
		Type: plugin.EventPracticePassed,
		Practice: &plugin.PracticePassedPayload{
			TargetLang: prof.ID,
			English:    english,
			Target:     target,
			Spoken:     spoken,
			Score:      score,
		},
	})
}

type translateTextInJSON struct {
	English         string   `json:"english"`
	Text            string   `json:"text"`
	SourceLanguage  string   `json:"source_language"`
	TargetLanguages []string `json:"target_languages"`
	FromSelf        bool     `json:"from_self"`
}

func (h *Host) handleTranslateText(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
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
	var in translateTextInJSON
	if err := json.Unmarshal(body, &in); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if len(in.TargetLanguages) > 0 {
		h.handleTranslateMulti(w, r, in)
		return
	}
	english := strings.TrimSpace(in.Text)
	if english == "" {
		english = strings.TrimSpace(in.English)
	}
	if english == "" {
		http.Error(w, "empty text", http.StatusBadRequest)
		return
	}
	if useNativeCloud() {
		out, err := nativecloud.TranslateEnglish(h.nativeSettingsFromDisk(), english)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		h.dispatchTranslationPlugins(r, out)
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	s := h.readSettingsFromDisk()
	prof := languages.Get(s.TargetLanguage)
	fwd := map[string]any{
		"english":                english,
		"src_nllb":               prof.SrcNLLB,
		"tgt_nllb":               prof.TgtNLLB,
		"back_src_nllb":          prof.BackSrcNLLB,
		"target_lang":            prof.ID,
		"backtranslate":          effectiveBacktranslate(s),
		"forward_translate":      s.ForwardTranslator,
		"openai_forward_model":   s.OpenAIForwardModel,
		"openai_backtrans_model": s.OpenAIBacktransModel,
		"api_provider":           s.APIProvider,
		"translate_model":        s.TranslateModel,
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
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, engineURL()+"/translate-text", bytes.NewReader(bout))
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
	if resp.StatusCode == http.StatusOK && len(respBody) > 0 {
		var payload map[string]any
		if json.Unmarshal(respBody, &payload) == nil {
			timing.LogTimingsMS("translate-text", payload)
			timing.LogEngineMeta("translate-text")
			h.dispatchTranslationPlugins(r, payload)
		}
	}
	_, _ = w.Write(respBody)
}

func (h *Host) handleTranslateMulti(w http.ResponseWriter, r *http.Request, in translateTextInJSON) {
	text := strings.TrimSpace(in.Text)
	if text == "" {
		text = strings.TrimSpace(in.English)
	}
	if text == "" {
		http.Error(w, "empty text", http.StatusBadRequest)
		return
	}
	s := h.readSettingsFromDisk()
	src := strings.TrimSpace(in.SourceLanguage)
	if src == "" {
		src = s.MyLanguage
	}
	srcCanon := languages.CanonicalID(src)

	if useNativeCloud() {
		results, err := nativecloud.TranslateMulti(h.nativeSettingsFromDisk(), src, in.TargetLanguages, text)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if in.FromSelf {
			dispatchConversationReply(r, srcCanon, text, results)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results, "source_language": srcCanon, "text": text})
		return
	}

	results := make([]map[string]any, 0, len(in.TargetLanguages))
	seen := map[string]bool{}
	for _, tgtID := range in.TargetLanguages {
		canon := languages.CanonicalID(tgtID)
		if canon == "" || canon == srcCanon || seen[canon] {
			continue
		}
		seen[canon] = true
		row, err := h.engineTranslatePair(r.Context(), s, srcCanon, canon, text)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		results = append(results, row)
	}
	if in.FromSelf {
		dispatchConversationReply(r, srcCanon, text, results)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"results": results, "source_language": srcCanon, "text": text})
}

func dispatchConversationReply(r *http.Request, srcLang, sourceText string, results []map[string]any) {
	lines := make([]plugin.ConversationLine, 0, len(results))
	for _, row := range results {
		lang, _ := row["language"].(string)
		label, _ := row["label"].(string)
		txt, _ := row["text"].(string)
		if strings.TrimSpace(txt) == "" {
			continue
		}
		lines = append(lines, plugin.ConversationLine{Lang: lang, Label: label, Text: txt})
	}
	if len(lines) == 0 {
		return
	}
	plugin.Default.Dispatch(r.Context(), plugin.Event{
		Type: plugin.EventConversationReply,
		Conversation: &plugin.ConversationPayload{
			SourceLang: srcLang,
			SourceText: sourceText,
			Lines:      lines,
		},
	})
}

func (h *Host) engineTranslatePair(ctx context.Context, s settingsFile, srcID, tgtID, text string) (map[string]any, error) {
	src := languages.LangOr(srcID)
	tgt := languages.LangOr(tgtID)
	fwd := map[string]any{
		"english":              text,
		"src_nllb":             src.NLLBCode,
		"tgt_nllb":             tgt.NLLBCode,
		"back_src_nllb":        tgt.NLLBCode,
		"target_lang":          tgt.ID,
		"backtranslate":        "none",
		"forward_translate":    s.ForwardTranslator,
		"openai_forward_model": s.OpenAIForwardModel,
		"api_provider":         s.APIProvider,
		"translate_model":      s.TranslateModel,
	}
	if key := strings.TrimSpace(s.OpenAIAPIKey); key != "" {
		fwd["openai_api_key"] = key
	}
	if s.OpenAIBaseURL != "" {
		fwd["openai_base_url"] = s.OpenAIBaseURL
	}
	if key := strings.TrimSpace(s.OpenRouterAPIKey); key != "" {
		fwd["openrouter_api_key"] = key
	}
	if s.OpenRouterBaseURL != "" {
		fwd["openrouter_base_url"] = s.OpenRouterBaseURL
	}
	bout, _ := json.Marshal(fwd)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, engineURL()+"/translate-text", bytes.NewReader(bout))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("engine translate %s→%s: %s", srcID, tgtID, strings.TrimSpace(string(rb)))
	}
	var payload map[string]any
	_ = json.Unmarshal(rb, &payload)
	translated, _ := payload["target"].(string)
	if translated == "" {
		translated, _ = payload["japanese"].(string)
	}
	row := map[string]any{
		"language":           tgt.ID,
		"label":              tgt.Label,
		"flag":               tgt.Flag,
		"text":               translated,
		"reading_aid":        string(tgt.ReadingAid),
		"reading_aid_tokens": []map[string]string{},
	}
	if tgt.ReadingAid == languages.ReadingAidFurigana {
		if raw, ok := payload["furigana"].([]any); ok {
			toks := make([]map[string]string, 0, len(raw))
			for _, item := range raw {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				surf, _ := m["surface"].(string)
				read, _ := m["reading"].(string)
				toks = append(toks, map[string]string{"surface": surf, "reading": read})
			}
			row["reading_aid_tokens"] = toks
		}
	}
	return row, nil
}

func (h *Host) handlePipeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if useNativeCloud() {
		wav, lang, err := readWAVFromMultipart(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if lang == "" {
			lang = languages.Get(h.readSettingsFromDisk().TargetLanguage).SourceASRLang
		}
		out, err := nativecloud.PipelineFromWAV(h.nativeSettingsFromDisk(), wav, lang)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		h.dispatchTranslationPlugins(r, out)
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	rec := &responseRecorder{header: make(http.Header)}
	h.forwardMultipart(rec, r, "/pipeline")
	for k, vv := range rec.header {
		if strings.EqualFold(k, "Content-Type") && len(vv) > 0 {
			w.Header().Set("Content-Type", vv[0])
		}
	}
	w.WriteHeader(rec.status)
	if rec.status == http.StatusOK && len(rec.body) > 0 {
		var payload map[string]any
		if json.Unmarshal(rec.body, &payload) == nil {
			timing.LogTimingsMS("pipeline", payload)
			timing.LogEngineMeta("pipeline")
			h.dispatchTranslationPlugins(r, payload)
		}
	}
	_, _ = w.Write(rec.body)
}

type responseRecorder struct {
	status int
	header http.Header
	body   []byte
}

func (rec *responseRecorder) Header() http.Header {
	return rec.header
}

func (rec *responseRecorder) Write(b []byte) (int, error) {
	rec.body = append(rec.body, b...)
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	return len(b), nil
}

func (rec *responseRecorder) WriteHeader(code int) {
	rec.status = code
}

func (h *Host) handleScore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.readSettingsFromDisk().PracticeEnabled {
		http.Error(w, "practice mode disabled", http.StatusBadRequest)
		return
	}
	const maxBody = 64 << 10
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	var exp, spk string
	var thr int
	var furi []scorepkg.FuriganaToken
	_ = json.Unmarshal(raw["expected"], &exp)
	_ = json.Unmarshal(raw["spoken"], &spk)
	_ = json.Unmarshal(raw["threshold"], &thr)
	if v, ok := raw["furigana"]; ok {
		_ = json.Unmarshal(v, &furi)
	}
	if thr <= 0 {
		thr = h.readSettingsFromDisk().ScoreThreshold
	}
	prof := languages.Get(h.readSettingsFromDisk().TargetLanguage)
	resp := scorepkg.Evaluate(scorepkg.Request{
		Expected:  strings.TrimSpace(exp),
		Spoken:    strings.TrimSpace(spk),
		Threshold: clampThreshold(thr),
		Furigana:  furi,
		Lang:      prof.ID,
	})
	w.Header().Set("Content-Type", "application/json")
	if resp.Accepted {
		h.dispatchPracticePassed(r, "", exp, spk, resp.Score)
	}
	_ = json.NewEncoder(w).Encode(resp)
}
