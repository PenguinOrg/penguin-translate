package host

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"translation-overlay/internal/feature/mictranslate/infra/languages"
	"translation-overlay/internal/feature/mictranslate/nativecloud"
	"translation-overlay/internal/platform/audio"
	"translation-overlay/internal/platform/cloudapi"
	"translation-overlay/internal/platform/engine"
)

func (h *Host) nativeSettingsFromDisk() nativecloud.Settings {
	s := h.readSettingsFromDisk()
	return nativecloud.Settings{
		TargetLanguage:            s.TargetLanguage,
		ForwardTranslator:         s.ForwardTranslator,
		EnglishASREngine:          s.EnglishASREngine,
		JaRepeatASREngine:         s.JaRepeatASREngine,
		Backtranslate:             s.Backtranslate,
		PracticeEnabled:           s.PracticeEnabled,
		APIProvider:               s.APIProvider,
		TranscribeModel:           s.TranscribeModel,
		TranslateModel:            s.TranslateModel,
		OpenAIForwardModel:        s.OpenAIForwardModel,
		OpenAIBacktransModel:      s.OpenAIBacktransModel,
		OpenAITranscribeModel:     s.OpenAITranscribeModel,
		OpenAIWhisperModel:        s.OpenAIWhisperModel,
		OpenRouterTranscribeModel: s.OpenRouterTranscribeModel,
		OpenAIKey:                 s.OpenAIAPIKey,
		OpenAIBase:                s.OpenAIBaseURL,
		OpenRouterKey:             s.OpenRouterAPIKey,
		OpenRouterBase:            s.OpenRouterBaseURL,
	}
}

func useNativeCloud() bool { return !engine.ManagedEngineAvailable() }

func (h *Host) handleNativeTranscribe(w http.ResponseWriter, r *http.Request) {
	wav, lang, err := readWAVFromMultipart(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if lang == "" {
		if l, ok := languages.Lang(h.readSettingsFromDisk().MyLanguage); ok {
			lang = l.ASRCode
		}
	}
	out, err := nativecloud.TranscribeWAV(h.nativeSettingsFromDisk(), wav, lang)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func readWAVFromMultipart(r *http.Request) ([]byte, string, error) {
	const max = 8 << 20
	if err := r.ParseMultipartForm(max); err != nil {
		return nil, "", err
	}
	lang := strings.TrimSpace(r.FormValue("language"))
	if lang == "" {
		lang = strings.TrimSpace(r.FormValue("speech_language"))
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		return nil, lang, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max))
	return b, lang, err
}

func handleNativeDevicesOutput(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"devices": audio.ListOutputDevices()})
}

func (h *Host) handleNativeSpeakTTS(w http.ResponseWriter, r *http.Request) {
	const max = 32 << 10
	body, err := io.ReadAll(io.LimitReader(r.Body, max))
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
	s := h.readSettingsFromDisk()
	ns := h.nativeSettingsFromDisk()
	raw, format, err := cloudapi.SynthesizeSpeech(
		ns.ToCredentials(),
		s.TTSEngine,
		s.OpenAITTSModel,
		s.TTSVoiceName,
		s.OpenAITTSInstructions,
		in.Text,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	switch format {
	case "pcm":
		err = audio.PlayPCM16LE(raw, 24000)
	default:
		path, err2 := writeTempFile(raw, "*.wav")
		if err2 != nil {
			http.Error(w, err2.Error(), http.StatusInternalServerError)
			return
		}
		defer os.Remove(path)
		err = audio.PlayWAV(path)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	dev := s.OutputDeviceName
	if dev == "" {
		dev = "System default"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "device_name": dev, "cached": false})
}

func handleNativePlayWAV(w http.ResponseWriter, r *http.Request) {
	wav, _, err := readWAVFromMultipart(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	path, err := writeTempFile(wav, "*.wav")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.Remove(path)
	if err := audio.PlayWAV(path); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func writeTempFile(data []byte, pattern string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}
