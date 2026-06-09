package domain

import "strings"

type LocalModelNeeds struct {
	Whisper bool
	NLLB    bool
}

func (n LocalModelNeeds) Any() bool { return n.Whisper || n.NLLB }

func MicTranslateLocalModelNeeds(p MicTranslateSettings) LocalModelNeeds {
	p = normalizePracticePolicy(p)
	var out LocalModelNeeds
	if normalizePipelineMode(p.PipelineMode) == "multimodal" {
		if effectiveBacktranslatePolicy(p) == "local" {
			out.NLLB = true
		}
		return out
	}
	if normalizeASREnginePolicy(p.EnglishASREngine) == "whisper" {
		out.Whisper = true
	}
	if strings.ToLower(strings.TrimSpace(p.ForwardTranslator)) == "nllb" {
		out.NLLB = true
	}
	if effectiveBacktranslatePolicy(p) == "local" {
		out.NLLB = true
	}
	return out
}

func WindowUsesLocalNLLB(w WindowSettings) bool {
	tb := strings.ToLower(strings.TrimSpace(w.TranslateBackend))
	return tb == "nllb" || tb == "local"
}

func RequiresManagedEngine(st Settings) bool {
	if MicTranslateLocalModelNeeds(st.MicTranslate).Any() {
		return true
	}
	if WindowUsesLocalNLLB(st.Window) {
		return true
	}
	return false
}

func normalizePipelineMode(v string) string {
	if strings.ToLower(strings.TrimSpace(v)) == "multimodal" {
		return "multimodal"
	}
	return "split"
}

func normalizeASREnginePolicy(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "openai", "openai_gpt", "gpt":
		return "openai"
	case "openai_whisper", "openai-whisper", "whisper-api", "whisper_api":
		return "openai_whisper"
	case "openrouter", "or":
		return "openrouter"
	default:
		return "whisper"
	}
}

func effectiveBacktranslatePolicy(p MicTranslateSettings) string {
	if !p.PracticeEnabled {
		return "none"
	}
	bt := strings.ToLower(strings.TrimSpace(p.Backtranslate))
	if bt != "none" && bt != "local" && bt != "openai" {
		return "local"
	}
	return bt
}

func normalizePracticePolicy(p MicTranslateSettings) MicTranslateSettings {
	ft := strings.ToLower(strings.TrimSpace(p.ForwardTranslator))
	if ft != "openai" {
		p.ForwardTranslator = "nllb"
	} else {
		p.ForwardTranslator = "openai"
	}
	p.EnglishASREngine = normalizeASREnginePolicy(p.EnglishASREngine)
	p.PipelineMode = normalizePipelineMode(p.PipelineMode)
	return p
}
