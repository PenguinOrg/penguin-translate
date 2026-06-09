package vrchatosc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/hypebeast/go-osc/osc"

	"translation-overlay/internal/feature/mictranslate/infra/plugin"
)

const pluginID = "vrchat_osc"

func init() {
	plugin.Register(func() plugin.Plugin { return &Plugin{} })
}

type Config struct {
	Enabled         bool   `json:"enabled"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Notification    bool   `json:"notification"`
	OnTranslation   bool   `json:"on_translation"`
	OnPass          bool   `json:"on_pass"`
	IncludeEnglish  bool   `json:"include_english"`
	IncludeOriginal bool   `json:"include_original"`
}

func defaultConfig() Config {
	return Config{
		Host:          "127.0.0.1",
		Port:          9000,
		Notification:  true,
		OnTranslation: false,
		OnPass:        true,
	}
}

type Plugin struct {
	mu  sync.RWMutex
	cfg Config
}

func (p *Plugin) Meta() plugin.Meta {
	return plugin.Meta{
		ID:          pluginID,
		Name:        "VRChat OSC chatbox",
		Description: "Send practice text to VRChat via OSC /chatbox/input (default port 9000).",
	}
}

func (p *Plugin) ApplyConfig(raw json.RawMessage) error {
	next := defaultConfig()
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &next); err != nil {
			return err
		}
	}
	if strings.TrimSpace(next.Host) == "" {
		next.Host = "127.0.0.1"
	}
	if next.Port <= 0 || next.Port > 65535 {
		next.Port = 9000
	}
	p.mu.Lock()
	p.cfg = next
	p.mu.Unlock()
	return nil
}

func (p *Plugin) PublicConfig() map[string]any {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return map[string]any{
		"enabled":          p.cfg.Enabled,
		"host":             p.cfg.Host,
		"port":             p.cfg.Port,
		"notification":     p.cfg.Notification,
		"on_translation":   p.cfg.OnTranslation,
		"on_pass":          p.cfg.OnPass,
		"include_english":  p.cfg.IncludeEnglish,
		"include_original": p.cfg.IncludeOriginal,
	}
}

func (p *Plugin) Handle(ctx context.Context, ev plugin.Event) error {
	_ = ctx
	p.mu.RLock()
	cfg := p.cfg
	p.mu.RUnlock()
	if !cfg.Enabled {
		return nil
	}
	var text string
	switch ev.Type {
	case plugin.EventConversationReply:
		if ev.Conversation == nil {
			return nil
		}
		text = composeConversationChatbox(cfg.IncludeOriginal, ev.Conversation)
	case plugin.EventTranslationReady:
		if !cfg.OnTranslation || ev.Translation == nil {
			return nil
		}
		text = composeChatboxText(cfg.IncludeEnglish, ev.Translation.English, ev.Translation.Target)
	case plugin.EventPracticePassed:
		if !cfg.OnPass || ev.Practice == nil {
			return nil
		}
		text = composeChatboxText(cfg.IncludeEnglish, ev.Practice.English, ev.Practice.Target)
	default:
		return nil
	}
	if text == "" {
		return nil
	}
	return Send(cfg.Host, cfg.Port, text, cfg.Notification)
}

func composeConversationChatbox(includeOriginal bool, c *plugin.ConversationPayload) string {
	lines := make([]string, 0, len(c.Lines)+1)
	if includeOriginal {
		if s := strings.TrimSpace(c.SourceText); s != "" {
			lines = append(lines, s)
		}
	}
	for _, l := range c.Lines {
		if t := strings.TrimSpace(l.Text); t != "" {
			lines = append(lines, t)
		}
	}
	return strings.Join(lines, "\n")
}

func composeChatboxText(includeEnglish bool, english, target string) string {
	english = strings.TrimSpace(english)
	target = strings.TrimSpace(target)
	if includeEnglish && english != "" {
		if target != "" {
			return english + "\n" + target
		}
		return english
	}
	return target
}

func formatChatboxText(s string) string {
	if strings.Contains(s, "\n") {
		parts := strings.Split(s, "\n")
		for i, p := range parts {
			parts[i] = formatSingleLineChatbox(p)
		}
		return strings.Join(parts, "\n")
	}
	return formatSingleLineChatbox(s)
}

func formatSingleLineChatbox(s string) string {
	s = strings.TrimSpace(s)
	for s != "" {
		r, w := utf8.DecodeLastRuneInString(s)
		if w == 0 {
			break
		}
		if r == '.' || r == '。' || r == '．' {
			s = strings.TrimSpace(s[:len(s)-w])
			continue
		}
		break
	}

	return strings.Join(strings.Fields(s), " ")
}

func Send(host string, port int, text string, notification bool) error {
	text = formatChatboxText(text)
	if text == "" {
		return fmt.Errorf("empty text")
	}
	host = strings.TrimSpace(host)
	if host == "" {
		host = "127.0.0.1"
	}
	if port <= 0 || port > 65535 {
		port = 9000
	}
	msg := osc.NewMessage("/chatbox/input")
	msg.Append(text)
	msg.Append(true)
	msg.Append(notification)
	client := osc.NewClient(host, port)
	if err := client.Send(msg); err != nil {
		return fmt.Errorf("osc send: %w", err)
	}
	return nil
}

func SendManual(cfg Config, text string) error {
	if !cfg.Enabled {
		return fmt.Errorf("plugin disabled")
	}
	return Send(cfg.Host, cfg.Port, text, cfg.Notification)
}

func ConfigFromPublic(m map[string]any) Config {
	cfg := defaultConfig()
	if m == nil {
		return cfg
	}
	if v, ok := m["enabled"].(bool); ok {
		cfg.Enabled = v
	}
	if v, ok := m["host"].(string); ok {
		cfg.Host = v
	}
	if v, ok := m["port"].(float64); ok {
		cfg.Port = int(v)
	}
	if v, ok := m["notification"].(bool); ok {
		cfg.Notification = v
	}
	if v, ok := m["on_translation"].(bool); ok {
		cfg.OnTranslation = v
	}
	if v, ok := m["on_pass"].(bool); ok {
		cfg.OnPass = v
	}
	if v, ok := m["include_english"].(bool); ok {
		cfg.IncludeEnglish = v
	}
	if v, ok := m["include_original"].(bool); ok {
		cfg.IncludeOriginal = v
	}
	return cfg
}
