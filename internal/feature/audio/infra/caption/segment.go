package caption

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"translation-overlay/internal/platform/cloudapi"
)

var transcribeSem sync.Mutex

type SegmentRequest struct {
	WAV              []byte
	WantDiarize      bool
	WantTranslate    bool
	VROverlayOn      bool
	Language         string
	Context          string
	TranslateContext string
	Pipeline         string
	Provider         string
	TranscribeModel  string
	DiarizeModel     string
	TranslateModel   string
	MultimodalModel  string
	Timeout          time.Duration
	Creds            cloudapi.Credentials
}

type Segment struct {
	Speaker  string     `json:"speaker"`
	Start    float64    `json:"start"`
	End      float64    `json:"end"`
	Text     string     `json:"text"`
	English  string     `json:"english"`
	Japanese string     `json:"japanese,omitempty"`
	Romaji   string     `json:"romaji,omitempty"`
	JPRomaji []RubyPair `json:"jp_romaji,omitempty"`
	ZhPinyin []RubyPair `json:"zh_pinyin,omitempty"`
	KoRoman  []RubyPair `json:"ko_roman,omitempty"`
}

type SegmentResponse struct {
	Diarized     bool             `json:"diarized"`
	Model        string           `json:"model"`
	Language     string           `json:"language"`
	Pipeline     string           `json:"pipeline"`
	FullText     string           `json:"full_text"`
	Segments     []Segment        `json:"segments"`
	Filtered     bool             `json:"filtered,omitempty"`
	FilterReason string           `json:"filter_reason,omitempty"`
	TimingsUS    map[string]int64 `json:"timings_us,omitempty"`
}

func TranscribeSegment(req SegmentRequest) (SegmentResponse, error) {
	transcribeSem.Lock()
	defer transcribeSem.Unlock()

	if IsAudioTooQuiet(req.WAV) {
		model := req.TranscribeModel
		if req.Pipeline == "multimodal" {
			model = req.MultimodalModel
		} else if req.WantDiarize {
			model = req.DiarizeModel
		}
		return emptyResponse(req.WantDiarize, model, req.Language, req.Pipeline, "audio_too_quiet"), nil
	}

	if req.Pipeline == "multimodal" {
		return transcribeMultimodal(req)
	}
	return transcribeSplit(req)
}

func transcribeMultimodal(req SegmentRequest) (SegmentResponse, error) {
	if req.WantDiarize {
		return SegmentResponse{}, fmt.Errorf("speaker diarization is not available in multimodal mode")
	}
	tMultimodal := time.Now()
	text, english, detLang, err := cloudapi.MultimodalCaptionWAV(
		req.Creds, req.MultimodalModel, req.Language, withRecentTranscript(req.Context), req.WAV, req.WantTranslate, req.Timeout,
	)
	multimodalUS := time.Since(tMultimodal).Microseconds()
	if err != nil {
		return SegmentResponse{}, err
	}
	model := req.MultimodalModel
	if text == "" {
		return SegmentResponse{
			Diarized: false, Model: model, Language: req.Language, Pipeline: "multimodal",
			FullText: "", Segments: nil,
		}, nil
	}
	if detLang != req.Language && (req.Language == "zh" || req.Language == "ja" || req.Language == "ko") {
		return emptyResponse(false, model, req.Language, "multimodal", "language_mismatch"), nil
	}
	if LanguageHintMismatch(text, req.Language) {
		return emptyResponse(false, model, req.Language, "multimodal", "language_mismatch"), nil
	}
	if LanguageHintMismatch(text, detLang) {
		return emptyResponse(false, model, detLang, "multimodal", "language_mismatch"), nil
	}
	enPreview := english
	if !req.WantTranslate {
		enPreview = ""
	}
	if reason := classifyWhisperArtifact(text, enPreview); reason != "" {
		return emptyResponse(false, model, req.Language, "multimodal", reason), nil
	}
	if reason := ClassifyInsignificantTranscript(text, enPreview); reason != "" {
		return emptyResponse(false, model, req.Language, "multimodal", reason), nil
	}
	ex := EnrichLine(text, effectiveLang(req.Language, text))
	seg := Segment{
		Speaker: "—", Text: StripLeadingPunctuation(text),
		English: StripLeadingPunctuation(english), Japanese: ex.Japanese, Romaji: ex.Romaji,
		JPRomaji: ex.JPRomaji, ZhPinyin: ex.ZhPinyin, KoRoman: ex.KoRoman,
	}
	pushHistory(seg.Text, seg.English)
	return SegmentResponse{
		Diarized: false, Model: model, Language: detLang, Pipeline: "multimodal",
		FullText: text, Segments: []Segment{seg},
		TimingsUS: map[string]int64{"transcribe": multimodalUS, "translate": 0},
	}, nil
}

func withRecentTranscript(base string) string {
	if strings.TrimSpace(base) == "" {
		return base
	}
	return strings.TrimSpace(base + recentSourceContext())
}

func transcribeSplit(req SegmentRequest) (SegmentResponse, error) {
	if req.WantDiarize && req.Provider != "openai" {
		return SegmentResponse{}, fmt.Errorf("speaker diarization requires OpenAI provider")
	}
	modelUse := req.TranscribeModel
	if req.WantDiarize {
		modelUse = req.DiarizeModel
	}
	var text string
	var rawSegs []map[string]any
	var err error
	tTranscribe := time.Now()
	switch req.Provider {
	case "dashscope":
		text, _, err = cloudapi.DashScopeTranscribeWAV(req.Creds, modelUse, req.Language, withRecentTranscript(req.Context), req.WAV, req.Timeout)
	case "openrouter":
		text, _, err = cloudapi.OpenRouterTranscribeWAV(req.Creds, modelUse, req.Language, req.WAV)
	default:
		text, rawSegs, err = cloudapi.OpenAITranscribeDetailed(req.Creds, modelUse, req.Language, req.WAV, req.WantDiarize, req.Timeout)
	}
	transcribeUS := time.Since(tTranscribe).Microseconds()
	if err != nil {
		return SegmentResponse{}, err
	}
	segs := normalizeSegments(req.WantDiarize, text, rawSegs)
	lines := make([]string, len(segs))
	for i, s := range segs {
		lines[i] = s.Text
	}
	combined := strings.TrimSpace(strings.Join(lines, " "))
	if combined == "" {
		combined = strings.TrimSpace(text)
	}
	detLang := effectiveLang(req.Language, combined)
	if LanguageHintMismatch(combined, req.Language) {
		return emptyResponse(req.WantDiarize, modelUse, req.Language, "split", "language_mismatch"), nil
	}
	if reason := classifyWhisperArtifact(combined, ""); reason != "" {
		return emptyResponse(req.WantDiarize, modelUse, req.Language, "split", reason), nil
	}
	if reason := ClassifyInsignificantTranscript(combined, ""); reason != "" {
		return emptyResponse(req.WantDiarize, modelUse, req.Language, "split", reason), nil
	}
	translations := make([]string, len(segs))
	var translateUS int64
	if req.WantTranslate && len(lines) > 0 {
		transCtx := strings.TrimSpace(req.TranslateContext)
		if transCtx != "" {
			if pc := recentPairContext(); pc != "" {
				transCtx += " Recent dialogue and its translations: " + pc
			}
		}
		tTranslate := time.Now()
		translations, err = cloudapi.BatchTranslateToEN(req.Creds, req.TranslateModel, detLang, transCtx, lines, req.Timeout)
		translateUS = time.Since(tTranslate).Microseconds()
		if err != nil {
			return SegmentResponse{}, err
		}
	}
	out := make([]Segment, 0, len(segs))
	for i, s := range segs {
		ex := EnrichLine(s.Text, effectiveLang(req.Language, s.Text))
		en := ""
		if i < len(translations) {
			en = translations[i]
		}
		textOut := StripLeadingPunctuation(s.Text)
		enOut := StripLeadingPunctuation(en)
		if reason := ClassifyInsignificantTranscript(textOut, enOut); reason != "" {
			continue
		}
		out = append(out, Segment{
			Speaker: s.Speaker, Start: s.Start, End: s.End,
			Text: textOut, English: enOut,
			Japanese: ex.Japanese, Romaji: ex.Romaji,
			JPRomaji: ex.JPRomaji, ZhPinyin: ex.ZhPinyin, KoRoman: ex.KoRoman,
		})
	}
	if len(out) == 0 {
		return emptyResponse(req.WantDiarize, modelUse, req.Language, "split", "filler"), nil
	}
	for _, s := range out {
		pushHistory(s.Text, s.English)
	}
	return SegmentResponse{
		Diarized: req.WantDiarize, Model: modelUse, Language: detLang, Pipeline: "split",
		FullText: text, Segments: out,
		TimingsUS: map[string]int64{"transcribe": transcribeUS, "translate": translateUS},
	}, nil
}

type rawSegment struct {
	Speaker string
	Start   float64
	End     float64
	Text    string
}

func normalizeSegments(diarize bool, text string, segments []map[string]any) []rawSegment {
	if diarize && len(segments) > 0 {
		var out []rawSegment
		for _, s := range segments {
			t := strings.TrimSpace(fmt.Sprint(s["text"]))
			if t == "" {
				continue
			}
			spk := strings.TrimSpace(fmt.Sprint(s["speaker"]))
			if spk == "" {
				spk = "—"
			}
			out = append(out, rawSegment{
				Speaker: spk,
				Start:   toFloat(s["start"]),
				End:     toFloat(s["end"]),
				Text:    t,
			})
		}
		if len(out) > 0 {
			return out
		}
	}
	t := strings.TrimSpace(text)
	if t == "" {
		return nil
	}
	return []rawSegment{{Speaker: "—", Text: t}}
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	default:
		return 0
	}
}

func emptyResponse(diarize bool, model, lang, pipe, reason string) SegmentResponse {
	return SegmentResponse{
		Diarized: diarize, Model: model, Language: lang, Pipeline: pipe,
		FullText: "", Segments: nil, Filtered: true, FilterReason: reason,
	}
}
