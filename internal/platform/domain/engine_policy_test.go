package domain

import "testing"

func TestMicTranslateLocalModelNeeds(t *testing.T) {
	tests := []struct {
		name        string
		in          MicTranslateSettings
		wantWhisper bool
		wantNLLB    bool
	}{
		{
			name: "all cloud, practice disabled",
			in: MicTranslateSettings{
				PracticeEnabled:   false,
				ForwardTranslator: "openai",
				EnglishASREngine:  "openai",
				PipelineMode:      "split",
				Backtranslate:     "local",
			},
		},
		{
			name: "split, local whisper ASR",
			in: MicTranslateSettings{
				PracticeEnabled:   true,
				ForwardTranslator: "openai",
				EnglishASREngine:  "whisper",
				PipelineMode:      "split",
				Backtranslate:     "openai",
			},
			wantWhisper: true,
		},
		{
			name: "split, nllb forward translator",
			in: MicTranslateSettings{
				PracticeEnabled:   true,
				ForwardTranslator: "nllb",
				EnglishASREngine:  "openai",
				PipelineMode:      "split",
				Backtranslate:     "none",
			},
			wantNLLB: true,
		},
		{
			name: "split, local backtranslate",
			in: MicTranslateSettings{
				PracticeEnabled:   true,
				ForwardTranslator: "openai",
				EnglishASREngine:  "openai",
				PipelineMode:      "split",
				Backtranslate:     "local",
			},
			wantNLLB: true,
		},
		{
			name: "split, unknown backtranslate coerced to local",
			in: MicTranslateSettings{
				PracticeEnabled:   true,
				ForwardTranslator: "openai",
				EnglishASREngine:  "openai",
				PipelineMode:      "split",
				Backtranslate:     "garbage",
			},
			wantNLLB: true,
		},
		{
			name: "split, whisper + nllb forward + local backtrans",
			in: MicTranslateSettings{
				PracticeEnabled:   true,
				ForwardTranslator: "nllb",
				EnglishASREngine:  "whisper",
				PipelineMode:      "split",
				Backtranslate:     "local",
			},
			wantWhisper: true,
			wantNLLB:    true,
		},
		{
			name: "multimodal ignores whisper ASR",
			in: MicTranslateSettings{
				PracticeEnabled:   true,
				ForwardTranslator: "openai",
				EnglishASREngine:  "whisper",
				PipelineMode:      "multimodal",
				Backtranslate:     "openai",
			},
		},
		{
			name: "multimodal with local backtranslate needs NLLB",
			in: MicTranslateSettings{
				PracticeEnabled:   true,
				ForwardTranslator: "openai",
				EnglishASREngine:  "openai",
				PipelineMode:      "multimodal",
				Backtranslate:     "local",
			},
			wantNLLB: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MicTranslateLocalModelNeeds(tt.in)
			if got.Whisper != tt.wantWhisper {
				t.Errorf("Whisper = %v, want %v", got.Whisper, tt.wantWhisper)
			}
			if got.NLLB != tt.wantNLLB {
				t.Errorf("NLLB = %v, want %v", got.NLLB, tt.wantNLLB)
			}
			if wantAny := tt.wantWhisper || tt.wantNLLB; got.Any() != wantAny {
				t.Errorf("Any() = %v, want %v", got.Any(), wantAny)
			}
		})
	}
}

func TestWindowUsesLocalNLLB(t *testing.T) {
	tests := []struct {
		backend string
		want    bool
	}{
		{"nllb", true},
		{"local", true},
		{"NLLB", true},
		{"  Local  ", true},
		{"openai", false},
		{" openrouter ", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.backend, func(t *testing.T) {
			if got := WindowUsesLocalNLLB(WindowSettings{TranslateBackend: tt.backend}); got != tt.want {
				t.Errorf("WindowUsesLocalNLLB(%q) = %v, want %v", tt.backend, got, tt.want)
			}
		})
	}
}

func TestRequiresManagedEngine(t *testing.T) {
	cloudMicTranslate := MicTranslateSettings{
		PracticeEnabled:   true,
		ForwardTranslator: "openai",
		EnglishASREngine:  "openai",
		PipelineMode:      "split",
		Backtranslate:     "openai",
	}
	cloudWindow := WindowSettings{TranslateBackend: "openai"}

	tests := []struct {
		name string
		st   Settings
		want bool
	}{
		{
			name: "all cloud",
			st:   Settings{MicTranslate: cloudMicTranslate, Window: cloudWindow},
			want: false,
		},
		{
			name: "practice needs local whisper",
			st: Settings{
				MicTranslate: MicTranslateSettings{
					PracticeEnabled:   true,
					ForwardTranslator: "openai",
					EnglishASREngine:  "whisper",
					PipelineMode:      "split",
					Backtranslate:     "openai",
				},
				Window: cloudWindow,
			},
			want: true,
		},
		{
			name: "window uses local nllb",
			st:   Settings{MicTranslate: cloudMicTranslate, Window: WindowSettings{TranslateBackend: "nllb"}},
			want: true,
		},
		{
			name: "audio settings never force the managed engine",
			st: Settings{
				MicTranslate: cloudMicTranslate,
				Window:       cloudWindow,
				Audio: AudioSettings{
					PipelineMode:   "split",
					TranslateModel: "gpt-4o-mini",
					DenoiseEnabled: true,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RequiresManagedEngine(tt.st); got != tt.want {
				t.Errorf("RequiresManagedEngine() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeASREnginePolicy(t *testing.T) {
	tests := map[string]string{
		"openai":         "openai",
		"openai_gpt":     "openai",
		"gpt":            "openai",
		"  GPT  ":        "openai",
		"openai_whisper": "openai_whisper",
		"openai-whisper": "openai_whisper",
		"whisper-api":    "openai_whisper",
		"whisper_api":    "openai_whisper",
		"openrouter":     "openrouter",
		"or":             "openrouter",
		"whisper":        "whisper",
		"":               "whisper",
		"something-else": "whisper",
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			if got := normalizeASREnginePolicy(in); got != want {
				t.Errorf("normalizeASREnginePolicy(%q) = %q, want %q", in, got, want)
			}
		})
	}
}

func TestNormalizePipelineMode(t *testing.T) {
	tests := map[string]string{
		"multimodal":   "multimodal",
		"  Multimodal": "multimodal",
		"split":        "split",
		"":             "split",
		"other":        "split",
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			if got := normalizePipelineMode(in); got != want {
				t.Errorf("normalizePipelineMode(%q) = %q, want %q", in, got, want)
			}
		})
	}
}

func TestEffectiveBacktranslatePolicy(t *testing.T) {
	tests := []struct {
		name string
		in   MicTranslateSettings
		want string
	}{
		{"disabled forces none", MicTranslateSettings{PracticeEnabled: false, Backtranslate: "local"}, "none"},
		{"enabled none", MicTranslateSettings{PracticeEnabled: true, Backtranslate: "none"}, "none"},
		{"enabled local", MicTranslateSettings{PracticeEnabled: true, Backtranslate: "local"}, "local"},
		{"enabled openai", MicTranslateSettings{PracticeEnabled: true, Backtranslate: "openai"}, "openai"},
		{"enabled trims+lowers", MicTranslateSettings{PracticeEnabled: true, Backtranslate: "  Local  "}, "local"},
		{"enabled empty coerces to local", MicTranslateSettings{PracticeEnabled: true, Backtranslate: ""}, "local"},
		{"enabled unknown coerces to local", MicTranslateSettings{PracticeEnabled: true, Backtranslate: "garbage"}, "local"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveBacktranslatePolicy(tt.in); got != tt.want {
				t.Errorf("effectiveBacktranslatePolicy() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizePracticePolicy(t *testing.T) {
	tests := []struct {
		name        string
		forward     string
		wantForward string
	}{
		{"openai stays openai", "openai", "openai"},
		{"OPENAI lowercased stays openai", "  OpenAI  ", "openai"},
		{"nllb stays nllb", "nllb", "nllb"},
		{"unknown becomes nllb", "whatever", "nllb"},
		{"empty becomes nllb", "", "nllb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizePracticePolicy(MicTranslateSettings{
				ForwardTranslator: tt.forward,
				EnglishASREngine:  "gpt",
				PipelineMode:      "  Multimodal",
			})
			if got.ForwardTranslator != tt.wantForward {
				t.Errorf("ForwardTranslator = %q, want %q", got.ForwardTranslator, tt.wantForward)
			}
			if got.EnglishASREngine != "openai" {
				t.Errorf("EnglishASREngine = %q, want normalized %q", got.EnglishASREngine, "openai")
			}
			if got.PipelineMode != "multimodal" {
				t.Errorf("PipelineMode = %q, want normalized %q", got.PipelineMode, "multimodal")
			}
		})
	}
}
