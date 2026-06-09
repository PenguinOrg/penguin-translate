package ocr

import "math"

type Box struct {
	X1, Y1, X2, Y2, X3, Y3, X4, Y4 float32
}

func (b Box) Scale(sx, sy float64) Box {
	return Box{
		X1: float32(float64(b.X1) * sx), Y1: float32(float64(b.Y1) * sy),
		X2: float32(float64(b.X2) * sx), Y2: float32(float64(b.Y2) * sy),
		X3: float32(float64(b.X3) * sx), Y3: float32(float64(b.Y3) * sy),
		X4: float32(float64(b.X4) * sx), Y4: float32(float64(b.Y4) * sy),
	}
}

func (b Box) Translate(dx, dy float32) Box {
	return Box{
		X1: b.X1 + dx, Y1: b.Y1 + dy,
		X2: b.X2 + dx, Y2: b.Y2 + dy,
		X3: b.X3 + dx, Y3: b.Y3 + dy,
		X4: b.X4 + dx, Y4: b.Y4 + dy,
	}
}

func (b Box) MinX() float32 {
	m := b.X1
	for _, x := range []float32{b.X2, b.X3, b.X4} {
		if x < m {
			m = x
		}
	}
	return m
}

func (b Box) AngleRadians() float64 {
	return math.Atan2(float64(b.Y2-b.Y1), float64(b.X2-b.X1))
}

func (b Box) Width() float64 {
	w1 := math.Hypot(float64(b.X2-b.X1), float64(b.Y2-b.Y1))
	w2 := math.Hypot(float64(b.X3-b.X4), float64(b.Y3-b.Y4))
	return (w1 + w2) / 2
}

func (b Box) Height() float64 {
	h1 := math.Hypot(float64(b.X4-b.X1), float64(b.Y4-b.Y1))
	h2 := math.Hypot(float64(b.X3-b.X2), float64(b.Y3-b.Y2))
	return (h1 + h2) / 2
}

func (b Box) Center() (float64, float64) {
	x := (float64(b.X1) + float64(b.X2) + float64(b.X3) + float64(b.X4)) / 4
	y := (float64(b.Y1) + float64(b.Y2) + float64(b.Y3) + float64(b.Y4)) / 4
	return x, y
}

type Line struct {
	Text string
	Box  Box
}

type Result struct {
	Lines    []Line
	FullText string
	CaptureW int
	CaptureH int
}
