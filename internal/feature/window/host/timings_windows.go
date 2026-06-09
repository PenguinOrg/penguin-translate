//go:build windows

package host

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"translation-overlay/internal/feature/window/infra/latency"
)

func handleWTTimings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"timings": latency.Snapshot()})
}

func handleWTTimingsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")

	ch := latency.Subscribe()
	defer latency.Unsubscribe(ch)

	if recent := latency.Snapshot(); len(recent) > 0 {
		writeWTTimingSSE(w, flusher, recent[0])
	}

	ctx := r.Context()
	ka := time.NewTicker(15 * time.Second)
	defer ka.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case rec := <-ch:
			writeWTTimingSSE(w, flusher, rec)
		case <-ka.C:
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func writeWTTimingSSE(w http.ResponseWriter, flusher http.Flusher, rec latency.Record) {
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_, _ = io.WriteString(w, "data: ")
	_, _ = w.Write(b)
	_, _ = io.WriteString(w, "\n\n")
	flusher.Flush()
}
