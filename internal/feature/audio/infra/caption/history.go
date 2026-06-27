package caption

import (
	"strings"
	"sync"
	"time"
)

type histEntry struct {
	source  string
	english string
	at      time.Time
}

const (
	histMaxEntries = 6
	histTTL        = 2 * time.Minute
)

var (
	histMu  sync.Mutex
	history []histEntry
)

func pushHistory(source, english string) {
	source = strings.TrimSpace(source)
	if source == "" {
		return
	}
	histMu.Lock()
	history = append(history, histEntry{source: source, english: strings.TrimSpace(english), at: time.Now()})
	if len(history) > histMaxEntries {
		history = history[len(history)-histMaxEntries:]
	}
	histMu.Unlock()
}

func recentHistory() []histEntry {
	histMu.Lock()
	defer histMu.Unlock()
	cutoff := time.Now().Add(-histTTL)
	out := make([]histEntry, 0, len(history))
	for _, e := range history {
		if e.at.After(cutoff) {
			out = append(out, e)
		}
	}
	return out
}

func ResetHistory() {
	histMu.Lock()
	history = nil
	histMu.Unlock()
}

func recentSourceContext() string {
	h := recentHistory()
	if len(h) == 0 {
		return ""
	}
	parts := make([]string, 0, len(h))
	for _, e := range h {
		parts = append(parts, e.source)
	}
	return " Recent dialogue (for continuity — keep names and terms consistent): " + strings.Join(parts, " / ")
}

func recentPairContext() string {
	h := recentHistory()
	if len(h) == 0 {
		return ""
	}
	if len(h) > 3 {
		h = h[len(h)-3:]
	}
	parts := make([]string, 0, len(h))
	for _, e := range h {
		if e.english == "" {
			continue
		}
		parts = append(parts, e.source+" => "+e.english)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " | ")
}
