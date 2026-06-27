package ocr

import "strings"

type FakeEngine struct {
	Lines []Line
}

func NewFakeEngine(lines ...string) *FakeEngine {
	if len(lines) == 0 {
		lines = []string{"これはテストです", "字幕のサンプル"}
	}
	ls := make([]Line, 0, len(lines))
	y := float32(10)
	for _, t := range lines {
		ls = append(ls, Line{
			Text: t,
			Box:  Box{X1: 10, Y1: y, X2: 210, Y2: y, X3: 210, Y3: y + 24, X4: 10, Y4: y + 24},
		})
		y += 40
	}
	return &FakeEngine{Lines: ls}
}

func (f *FakeEngine) RecognizeResult(pixels []byte, captureW, captureH int) (*Result, error) {
	lines := append([]Line(nil), f.Lines...)
	parts := make([]string, len(lines))
	for i, l := range lines {
		parts[i] = l.Text
	}
	return &Result{
		Lines:    lines,
		FullText: strings.Join(parts, "\n"),
		CaptureW: captureW,
		CaptureH: captureH,
	}, nil
}

func (f *FakeEngine) Close() {}
