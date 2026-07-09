package host

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"translation-overlay/internal/platform/cloudapi"
	"translation-overlay/internal/platform/lang/languages"
)

func (s settingsFile) ttsCredentials() cloudapi.Credentials {
	return cloudapi.Credentials{
		OpenAIKey:      s.OpenAIAPIKey,
		OpenAIBase:     s.OpenAIBaseURL,
		OpenRouterKey:  s.OpenRouterAPIKey,
		OpenRouterBase: s.OpenRouterBaseURL,
		DeepgramKey:    s.DeepgramAPIKey,
		DeepgramBase:   s.DeepgramBaseURL,
		DashScopeKey:   s.DashScopeAPIKey,
		DashScopeBase:  s.DashScopeBaseURL,
	}
}

// handlePracticeTTS synthesizes the practice phrase and returns the audio bytes
// for the browser to play locally — distinct from /api/speak-tts, which plays
// server-side through the configured output device (the VRChat/speaker route).
func (h *Host) handlePracticeTTS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	const maxBody = 16 << 10
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var in struct {
		Text string `json:"text"`
	}
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
	raw, format, err := cloudapi.SynthesizeSpeech(s.ttsCredentials(), s.TTSEngine, s.OpenAITTSModel, s.TTSVoiceName, s.OpenAITTSInstructions, text)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	ct := "audio/wav"
	switch format {
	case "pcm":
		raw = pcm16ToWAV(raw, 24000)
	case "mp3":
		ct = "audio/mpeg"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(raw)
}

// handleAssess runs Azure pronunciation assessment on the spoken clip.
func (h *Host) handleAssess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s := h.readSettingsFromDisk()
	if !s.PracticeEnabled {
		http.Error(w, "practice mode disabled", http.StatusBadRequest)
		return
	}
	wav, lang, err := readWAVFromMultipart(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	expected := strings.TrimSpace(r.FormValue("expected"))
	if expected == "" {
		http.Error(w, "expected text required", http.StatusBadRequest)
		return
	}
	if lang == "" {
		if l, ok := languages.Lang(s.TargetLanguage); ok {
			lang = l.ASRCode
		}
	}
	thr := s.ScoreThreshold
	if v, e := strconv.Atoi(strings.TrimSpace(r.FormValue("threshold"))); e == nil && v > 0 {
		thr = clampThreshold(v)
	}

	// Azure expects the catalog's BCP-47 TTS locale.
	locale := languages.LangOr(lang).TTSLang
	res, err := cloudapi.AssessPronunciation(s.AzureSpeechRegion, s.AzureSpeechKey, locale, expected, wav)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	overall := res.Pron
	if overall <= 0 {
		overall = res.Accuracy
	}
	recognized := strings.TrimSpace(res.RecognizedText)
	accepted := recognized != "" && int(math.Round(overall)) >= thr

	words := make([]map[string]any, 0, len(res.Words))
	for _, wd := range res.Words {
		words = append(words, map[string]any{"word": wd.Word, "accuracy": wd.Accuracy, "error_type": wd.ErrorType})
	}
	out := map[string]any{
		"score":           int(math.Round(overall)),
		"accepted":        accepted,
		"recognized_text": recognized,
		"sub": map[string]any{
			"accuracy":     res.Accuracy,
			"fluency":      res.Fluency,
			"completeness": res.Completeness,
			"prosody":      res.Prosody,
		},
		"words": words,
	}
	if accepted {
		h.dispatchPracticePassed(r, "", expected, recognized, overall)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// pcm16ToWAV wraps signed-16-bit little-endian mono PCM in a minimal WAV container.
func pcm16ToWAV(pcm []byte, sampleRate int) []byte {
	const numChannels, bitsPerSample = 1, 16
	byteRate := sampleRate * numChannels * bitsPerSample / 8
	blockAlign := numChannels * bitsPerSample / 8
	buf := make([]byte, 0, 44+len(pcm))
	buf = append(buf, []byte("RIFF")...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(36+len(pcm)))
	buf = append(buf, []byte("WAVE")...)
	buf = append(buf, []byte("fmt ")...)
	buf = binary.LittleEndian.AppendUint32(buf, 16)
	buf = binary.LittleEndian.AppendUint16(buf, 1) // PCM
	buf = binary.LittleEndian.AppendUint16(buf, uint16(numChannels))
	buf = binary.LittleEndian.AppendUint32(buf, uint32(sampleRate))
	buf = binary.LittleEndian.AppendUint32(buf, uint32(byteRate))
	buf = binary.LittleEndian.AppendUint16(buf, uint16(blockAlign))
	buf = binary.LittleEndian.AppendUint16(buf, bitsPerSample)
	buf = append(buf, []byte("data")...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(pcm)))
	buf = append(buf, pcm...)
	return buf
}
