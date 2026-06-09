package latency

import (
	"sync"
	"sync/atomic"
	"time"
)

type SideTiming struct {
	SpansUS map[string]int64 `json:"spans_us"`
	TotalUS int64            `json:"total_us"`
}

type Record struct {
	FrameID  uint64      `json:"frame_id"`
	TSUnixMS int64       `json:"ts_unix_ms"`
	Runner   *SideTiming `json:"runner,omitempty"`
	Overlay  *SideTiming `json:"overlay,omitempty"`
}

const ringCap = 256

type store struct {
	mu    sync.Mutex
	ring  []*Record
	index map[uint64]*Record
	subs  map[chan Record]struct{}
}

var (
	frameCounter atomic.Uint64
	def          = &store{
		index: map[uint64]*Record{},
		subs:  map[chan Record]struct{}{},
	}
)

func NextFrameID() uint64 { return frameCounter.Add(1) }

func US(start time.Time) int64 { return time.Since(start).Microseconds() }

func RecordRunner(frameID uint64, spans map[string]int64) {
	def.record(frameID, spans, true)
}

func RecordOverlay(frameID uint64, spans map[string]int64) {
	def.record(frameID, spans, false)
}

func (s *store) record(frameID uint64, spans map[string]int64, isRunner bool) {
	if frameID == 0 || len(spans) == 0 {
		return
	}
	s.mu.Lock()
	rec := s.index[frameID]
	if rec == nil {
		rec = &Record{FrameID: frameID, TSUnixMS: time.Now().UnixMilli()}
		s.index[frameID] = rec
		s.ring = append(s.ring, rec)
		if len(s.ring) > ringCap {
			old := s.ring[0]
			s.ring = s.ring[1:]
			delete(s.index, old.FrameID)
		}
	}
	side := &rec.Runner
	if !isRunner {
		side = &rec.Overlay
	}
	if *side == nil {
		*side = &SideTiming{SpansUS: map[string]int64{}}
	}
	for k, v := range spans {
		(*side).SpansUS[k] = v
	}
	var total int64
	for _, v := range (*side).SpansUS {
		total += v
	}
	(*side).TotalUS = total

	snapshot := cloneRecord(rec)
	subs := make([]chan Record, 0, len(s.subs))
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

func cloneRecord(r *Record) Record {
	out := Record{FrameID: r.FrameID, TSUnixMS: r.TSUnixMS}
	if r.Runner != nil {
		out.Runner = cloneSide(r.Runner)
	}
	if r.Overlay != nil {
		out.Overlay = cloneSide(r.Overlay)
	}
	return out
}

func cloneSide(s *SideTiming) *SideTiming {
	spans := make(map[string]int64, len(s.SpansUS))
	for k, v := range s.SpansUS {
		spans[k] = v
	}
	return &SideTiming{SpansUS: spans, TotalUS: s.TotalUS}
}

func Snapshot() []Record {
	def.mu.Lock()
	defer def.mu.Unlock()
	out := make([]Record, 0, len(def.ring))
	for i := len(def.ring) - 1; i >= 0; i-- {
		out = append(out, cloneRecord(def.ring[i]))
	}
	return out
}

func Subscribe() chan Record {
	ch := make(chan Record, 32)
	def.mu.Lock()
	def.subs[ch] = struct{}{}
	def.mu.Unlock()
	return ch
}

func Unsubscribe(ch chan Record) {
	def.mu.Lock()
	delete(def.subs, ch)
	def.mu.Unlock()
	close(ch)
}
