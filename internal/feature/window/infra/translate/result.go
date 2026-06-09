package translate

import (
	"encoding/json"
	"strings"

	"translation-overlay/internal/feature/window/infra/cache"
)

type LineResult struct {
	En    string `json:"en"`
	Roman string `json:"roman"`
}

func getCached(store *cache.Store, line string) (LineResult, bool) {
	if store == nil {
		return LineResult{}, false
	}
	raw, ok := store.Get(line)
	if !ok {
		return LineResult{}, false
	}
	return decodeStored(raw), true
}

func putCached(store *cache.Store, line string, r LineResult) {
	if store == nil || IsRefusal(r.En) {
		return
	}
	_ = store.Put(line, encodeStored(r))
}

func encodeStored(r LineResult) string {
	b, err := json.Marshal(r)
	if err != nil {
		return r.En
	}
	return string(b)
}

func decodeStored(raw string) LineResult {
	var r LineResult
	if json.Unmarshal([]byte(raw), &r) == nil && strings.TrimSpace(r.En) != "" {
		return r
	}
	return LineResult{En: raw}
}

func SanitizeRoman(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) > 160 {
		s = s[:157] + "..."
	}
	return s
}
