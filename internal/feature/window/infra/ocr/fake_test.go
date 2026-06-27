package ocr

import (
	"strings"
	"testing"
)

func TestFakeEngineReturnsFixtureLines(t *testing.T) {
	e := NewFakeEngine("これはテストです", "字幕のサンプル")
	res, err := e.RecognizeResult([]byte{1, 2, 3}, 640, 480)
	if err != nil {
		t.Fatalf("RecognizeResult: %v", err)
	}
	if len(res.Lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %+v", len(res.Lines), res.Lines)
	}
	if res.Lines[0].Text != "これはテストです" {
		t.Errorf("line 0 = %q", res.Lines[0].Text)
	}
	if !strings.Contains(res.FullText, "字幕のサンプル") {
		t.Errorf("FullText missing second line: %q", res.FullText)
	}
	if res.CaptureW != 640 || res.CaptureH != 480 {
		t.Errorf("capture dims = %dx%d, want 640x480", res.CaptureW, res.CaptureH)
	}
	if res.Lines[0].Box.Width() <= 0 || res.Lines[0].Box.Height() <= 0 {
		t.Errorf("line 0 box is degenerate: %+v", res.Lines[0].Box)
	}
}

func TestFakeEngineDefaults(t *testing.T) {
	e := NewFakeEngine()
	res, _ := e.RecognizeResult(nil, 100, 100)
	if len(res.Lines) == 0 {
		t.Fatal("default FakeEngine produced no lines")
	}
}

var _ Recognizer = (*FakeEngine)(nil)
