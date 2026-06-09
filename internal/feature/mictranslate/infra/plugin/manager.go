package plugin

import (
	"context"
	"encoding/json"
	"log"
	"sync"
)

type Manager struct {
	mu      sync.RWMutex
	ordered []string
	byID    map[string]Plugin
}

var Default = &Manager{byID: make(map[string]Plugin)}

func Register(factory Factory) {
	p := factory()
	id := p.Meta().ID
	Default.mu.Lock()
	defer Default.mu.Unlock()
	if _, dup := Default.byID[id]; dup {
		log.Printf("plugin: duplicate id %q ignored", id)
		return
	}
	Default.byID[id] = p
	Default.ordered = append(Default.ordered, id)
}

func (m *Manager) List() []Meta {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Meta, 0, len(m.ordered))
	for _, id := range m.ordered {
		if p, ok := m.byID[id]; ok {
			out = append(out, p.Meta())
		}
	}
	return out
}

func (m *Manager) ApplyAllConfigs(configs map[string]json.RawMessage) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id, p := range m.byID {
		raw := configs[id]
		if len(raw) == 0 {
			raw = json.RawMessage(`{}`)
		}
		if err := p.ApplyConfig(raw); err != nil {
			log.Printf("plugin %s config: %v", id, err)
		}
	}
}

func (m *Manager) PublicConfigs() map[string]map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]map[string]any, len(m.byID))
	for id, p := range m.byID {
		out[id] = p.PublicConfig()
	}
	return out
}

func (m *Manager) Dispatch(ctx context.Context, ev Event) {
	m.mu.RLock()
	ids := append([]string(nil), m.ordered...)
	m.mu.RUnlock()
	for _, id := range ids {
		m.mu.RLock()
		p, ok := m.byID[id]
		m.mu.RUnlock()
		if !ok {
			continue
		}
		if err := p.Handle(ctx, ev); err != nil {
			log.Printf("plugin %s: %v", id, err)
		}
	}
}
