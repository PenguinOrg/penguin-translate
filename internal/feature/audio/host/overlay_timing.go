package host

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type stageSpans map[string]int64

func (s stageSpans) total() int64 {
	var t int64
	for _, v := range s {
		t += v
	}
	return t
}

var reqIDCounter atomic.Uint64

func nextReqID() uint64 { return reqIDCounter.Add(1) }

func usSince(t time.Time) int64 { return time.Since(t).Microseconds() }

type sideTiming struct {
	SpansUS stageSpans `json:"spans_us"`
	TotalUS int64      `json:"total_us"`
}

type mergedRecord struct {
	ReqID    uint64      `json:"req_id"`
	TSUnixMS int64       `json:"ts_unix_ms"`
	Go       *sideTiming `json:"go,omitempty"`
	Rust     *sideTiming `json:"rust,omitempty"`
}

const overlayTimingsCap = 256

type timingStore struct {
	mu    sync.Mutex
	ring  []*mergedRecord
	index map[uint64]*mergedRecord
	subs  map[chan mergedRecord]struct{}
}

var overlayTimings = &timingStore{
	index: map[uint64]*mergedRecord{},
	subs:  map[chan mergedRecord]struct{}{},
}

func (s *timingStore) recordGo(reqID uint64, spans stageSpans, total int64) {
	s.record(reqID, &sideTiming{SpansUS: spans, TotalUS: total}, true)
}

func (s *timingStore) recordRust(reqID uint64, spans stageSpans, total int64) {
	s.record(reqID, &sideTiming{SpansUS: spans, TotalUS: total}, false)
}

func (s *timingStore) record(reqID uint64, side *sideTiming, isGo bool) {
	if reqID == 0 {
		return
	}
	s.mu.Lock()
	rec := s.index[reqID]
	if rec == nil {
		rec = &mergedRecord{ReqID: reqID, TSUnixMS: time.Now().UnixMilli()}
		s.index[reqID] = rec
		s.ring = append(s.ring, rec)
		if len(s.ring) > overlayTimingsCap {
			old := s.ring[0]
			s.ring = s.ring[1:]
			delete(s.index, old.ReqID)
		}
	}
	if isGo {
		rec.Go = side
	} else {
		rec.Rust = side
	}
	snapshot := *rec
	subs := make([]chan mergedRecord, 0, len(s.subs))
	for ch := range s.subs {
		subs = append(subs, ch)
	}
	s.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- snapshot:
		default:
		}
	}
}

func (s *timingStore) snapshot() []mergedRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]mergedRecord, 0, len(s.ring))
	for i := len(s.ring) - 1; i >= 0; i-- {
		out = append(out, *s.ring[i])
	}
	return out
}

func (s *timingStore) subscribe() chan mergedRecord {
	ch := make(chan mergedRecord, 32)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

func (s *timingStore) unsubscribe(ch chan mergedRecord) {
	s.mu.Lock()
	delete(s.subs, ch)
	s.mu.Unlock()
	close(ch)
}

func logCaptionTiming(side string, reqID uint64, spans stageSpans, total int64) {
	b, err := json.Marshal(struct {
		Event   string     `json:"event"`
		Side    string     `json:"side"`
		ReqID   uint64     `json:"req_id"`
		SpansUS stageSpans `json:"spans_us"`
		TotalUS int64      `json:"total_us"`
	}{"caption_timing", side, reqID, spans, total})
	if err == nil {
		writeOverlayLog("timing", string(b))
	}
}

func handleOverlayTimings(w http.ResponseWriter, r *http.Request) {
	if !isGetOrHead(r) {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"timings": overlayTimings.snapshot()})
}

func handleOverlayTimingsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")

	ch := overlayTimings.subscribe()
	defer overlayTimings.unsubscribe(ch)

	if recent := overlayTimings.snapshot(); len(recent) > 0 {
		writeSSE(w, flusher, recent[0])
	}

	ctx := r.Context()
	ka := time.NewTicker(15 * time.Second)
	defer ka.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case rec := <-ch:
			writeSSE(w, flusher, rec)
		case <-ka.C:
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, rec mergedRecord) {
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_, _ = io.WriteString(w, "data: ")
	_, _ = w.Write(b)
	_, _ = io.WriteString(w, "\n\n")
	flusher.Flush()
}
