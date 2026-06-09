package latency

import "testing"

func findByID(snap []Record, id uint64) (Record, bool) {
	for _, r := range snap {
		if r.FrameID == id {
			return r, true
		}
	}
	return Record{}, false
}

func TestRecordMergesSidesByFrame(t *testing.T) {
	id := NextFrameID()
	RecordRunner(id, map[string]int64{"capture": 100, "framehash": 10, "ocr": 5000, "resolve": 40})
	RecordRunner(id, map[string]int64{"translate": 20000})
	RecordOverlay(id, map[string]int64{"render": 800, "present": 300})

	rec, ok := findByID(Snapshot(), id)
	if !ok {
		t.Fatalf("frame %d not found in snapshot", id)
	}
	if rec.Runner == nil || rec.Overlay == nil {
		t.Fatalf("expected both sides populated, got runner=%v overlay=%v", rec.Runner, rec.Overlay)
	}
	if got := rec.Runner.SpansUS["translate"]; got != 20000 {
		t.Errorf("merged translate span = %d, want 20000", got)
	}
	if got := rec.Runner.SpansUS["capture"]; got != 100 {
		t.Errorf("capture span lost after merge = %d, want 100", got)
	}
	if want := int64(100 + 10 + 5000 + 40 + 20000); rec.Runner.TotalUS != want {
		t.Errorf("runner total = %d, want %d", rec.Runner.TotalUS, want)
	}
	if want := int64(800 + 300); rec.Overlay.TotalUS != want {
		t.Errorf("overlay total = %d, want %d", rec.Overlay.TotalUS, want)
	}
}

func TestZeroFrameIDIgnored(t *testing.T) {
	before := len(Snapshot())
	RecordRunner(0, map[string]int64{"capture": 1})
	RecordOverlay(0, map[string]int64{"render": 1})
	if after := len(Snapshot()); after != before {
		t.Errorf("zero frame id was recorded: snapshot grew %d -> %d", before, after)
	}
}

func TestSnapshotNewestFirst(t *testing.T) {
	a := NextFrameID()
	RecordRunner(a, map[string]int64{"capture": 1})
	b := NextFrameID()
	RecordRunner(b, map[string]int64{"capture": 1})

	snap := Snapshot()
	if len(snap) < 2 {
		t.Fatalf("snapshot too short: %d", len(snap))
	}
	if snap[0].FrameID != b || snap[1].FrameID != a {
		t.Errorf("newest-first order wrong: got %d,%d want %d,%d", snap[0].FrameID, snap[1].FrameID, b, a)
	}
}

func TestSnapshotDoesNotAliasSpans(t *testing.T) {
	id := NextFrameID()
	RecordRunner(id, map[string]int64{"capture": 100})

	rec, ok := findByID(Snapshot(), id)
	if !ok {
		t.Fatalf("frame %d not found", id)
	}
	RecordRunner(id, map[string]int64{"capture": 999})
	if rec.Runner.SpansUS["capture"] != 100 {
		t.Errorf("earlier snapshot aliased live store: capture = %d, want 100", rec.Runner.SpansUS["capture"])
	}
}
