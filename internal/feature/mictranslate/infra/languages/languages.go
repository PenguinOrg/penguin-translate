package languages

import "strings"

type Profile struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	ShortLabel    string `json:"short_label"`
	SourceASRLang string `json:"source_asr_lang"`
	TargetASRLang string `json:"target_asr_lang"`
	SrcNLLB       string `json:"src_nllb"`
	TgtNLLB       string `json:"tgt_nllb"`
	TTSLang       string `json:"tts_lang"`
	HasFurigana   bool   `json:"has_furigana"`
	ScorePath     string `json:"score_path"`
	BackSrcNLLB   string `json:"back_src_nllb"`
}

var profiles = []Profile{
	{
		ID:            "jp",
		Label:         "Japanese",
		ShortLabel:    "JP",
		SourceASRLang: "en",
		TargetASRLang: "ja",
		SrcNLLB:       "eng_Latn",
		TgtNLLB:       "jpn_Jpan",
		TTSLang:       "ja-JP",
		HasFurigana:   true,
		ScorePath:     "/score-ja",
		BackSrcNLLB:   "jpn_Jpan",
	},
	{
		ID:            "zh",
		Label:         "Chinese (Simplified)",
		ShortLabel:    "ZH",
		SourceASRLang: "en",
		TargetASRLang: "zh",
		SrcNLLB:       "eng_Latn",
		TgtNLLB:       "zho_Hans",
		TTSLang:       "zh-CN",
		HasFurigana:   false,
		ScorePath:     "/score-zh",
		BackSrcNLLB:   "zho_Hans",
	},
	{
		ID:            "ko",
		Label:         "Korean",
		ShortLabel:    "KO",
		SourceASRLang: "en",
		TargetASRLang: "ko",
		SrcNLLB:       "eng_Latn",
		TgtNLLB:       "kor_Hang",
		TTSLang:       "ko-KR",
		HasFurigana:   false,
		ScorePath:     "/score-ko",
		BackSrcNLLB:   "kor_Hang",
	},
}

func All() []Profile {
	out := make([]Profile, len(profiles))
	copy(out, profiles)
	return out
}

func Get(id string) Profile {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, p := range profiles {
		if p.ID == id {
			return p
		}
	}
	return profiles[0]
}

func NormalizeID(id string) string {
	return Get(id).ID
}

type ReadingAid string

const (
	ReadingAidNone     ReadingAid = "none"
	ReadingAidFurigana ReadingAid = "furigana"
	ReadingAidPinyin   ReadingAid = "pinyin"
	ReadingAidRomaja   ReadingAid = "romaja"
)

type Language struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	ShortLabel string     `json:"short_label"`
	Flag       string     `json:"flag"`
	ASRCode    string     `json:"asr_code"`
	NLLBCode   string     `json:"nllb_code"`
	TTSLang    string     `json:"tts_lang"`
	ReadingAid ReadingAid `json:"reading_aid"`
}

var registry = []Language{
	{ID: "en", Label: "English", ShortLabel: "EN", Flag: "🇬🇧", ASRCode: "en", NLLBCode: "eng_Latn", TTSLang: "en-US", ReadingAid: ReadingAidNone},
	{ID: "ja", Label: "Japanese", ShortLabel: "JA", Flag: "🇯🇵", ASRCode: "ja", NLLBCode: "jpn_Jpan", TTSLang: "ja-JP", ReadingAid: ReadingAidFurigana},
	{ID: "zh", Label: "Chinese (Simplified)", ShortLabel: "ZH", Flag: "🇨🇳", ASRCode: "zh", NLLBCode: "zho_Hans", TTSLang: "zh-CN", ReadingAid: ReadingAidPinyin},
	{ID: "ko", Label: "Korean", ShortLabel: "KO", Flag: "🇰🇷", ASRCode: "ko", NLLBCode: "kor_Hang", TTSLang: "ko-KR", ReadingAid: ReadingAidRomaja},
	{ID: "es", Label: "Spanish", ShortLabel: "ES", Flag: "🇪🇸", ASRCode: "es", NLLBCode: "spa_Latn", TTSLang: "es-ES", ReadingAid: ReadingAidNone},
	{ID: "fr", Label: "French", ShortLabel: "FR", Flag: "🇫🇷", ASRCode: "fr", NLLBCode: "fra_Latn", TTSLang: "fr-FR", ReadingAid: ReadingAidNone},
	{ID: "de", Label: "German", ShortLabel: "DE", Flag: "🇩🇪", ASRCode: "de", NLLBCode: "deu_Latn", TTSLang: "de-DE", ReadingAid: ReadingAidNone},
	{ID: "it", Label: "Italian", ShortLabel: "IT", Flag: "🇮🇹", ASRCode: "it", NLLBCode: "ita_Latn", TTSLang: "it-IT", ReadingAid: ReadingAidNone},
	{ID: "pt", Label: "Portuguese", ShortLabel: "PT", Flag: "🇵🇹", ASRCode: "pt", NLLBCode: "por_Latn", TTSLang: "pt-PT", ReadingAid: ReadingAidNone},
	{ID: "ru", Label: "Russian", ShortLabel: "RU", Flag: "🇷🇺", ASRCode: "ru", NLLBCode: "rus_Cyrl", TTSLang: "ru-RU", ReadingAid: ReadingAidNone},
}

var langAliases = map[string]string{
	"jp": "ja", "jpn": "ja", "japanese": "ja",
	"zh": "zh", "cn": "zh", "zho": "zh", "chinese": "zh", "zh-cn": "zh", "zh_hans": "zh",
	"ko": "ko", "kr": "ko", "kor": "ko", "korean": "ko",
	"en": "en", "eng": "en", "english": "en",
	"es": "es", "spa": "es", "spanish": "es",
	"fr": "fr", "fra": "fr", "french": "fr",
	"de": "de", "deu": "de", "ger": "de", "german": "de",
	"it": "it", "ita": "it", "italian": "it",
	"pt": "pt", "por": "pt", "portuguese": "pt",
	"ru": "ru", "rus": "ru", "russian": "ru",
}

func CanonicalID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if c, ok := langAliases[id]; ok {
		return c
	}
	return id
}

func Catalog() []Language {
	out := make([]Language, len(registry))
	copy(out, registry)
	return out
}

func Lang(id string) (Language, bool) {
	cid := CanonicalID(id)
	for _, l := range registry {
		if l.ID == cid {
			return l, true
		}
	}
	return Language{}, false
}

func LangOr(id string) Language {
	if l, ok := Lang(id); ok {
		return l
	}
	en, _ := Lang("en")
	return en
}
