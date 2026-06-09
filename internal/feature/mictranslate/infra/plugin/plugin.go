package plugin

import (
	"context"
	"encoding/json"
)

type EventType string

const (
	EventTranslationReady  EventType = "translation_ready"
	EventPracticePassed    EventType = "practice_passed"
	EventConversationReply EventType = "conversation_reply"
)

type FuriganaToken struct {
	Surface string `json:"surface"`
	Reading string `json:"reading"`
}

type TranslationPayload struct {
	TargetLang  string          `json:"target_lang"`
	English     string          `json:"english"`
	Target      string          `json:"target"`
	BackEnglish string          `json:"back_english"`
	Furigana    []FuriganaToken `json:"furigana,omitempty"`
}

type PracticePassedPayload struct {
	TargetLang string  `json:"target_lang"`
	English    string  `json:"english"`
	Target     string  `json:"target"`
	Spoken     string  `json:"spoken"`
	Score      float64 `json:"score"`
}

type ConversationLine struct {
	Lang  string `json:"lang"`
	Label string `json:"label"`
	Text  string `json:"text"`
}

type ConversationPayload struct {
	SourceLang string             `json:"source_lang"`
	SourceText string             `json:"source_text"`
	Lines      []ConversationLine `json:"lines"`
}

type Event struct {
	Type         EventType
	Translation  *TranslationPayload
	Practice     *PracticePassedPayload
	Conversation *ConversationPayload
}

type Meta struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Plugin interface {
	Meta() Meta

	ApplyConfig(raw json.RawMessage) error

	PublicConfig() map[string]any

	Handle(ctx context.Context, ev Event) error
}

type Factory func() Plugin
