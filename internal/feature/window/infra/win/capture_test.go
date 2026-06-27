//go:build windows

package win

import "testing"

func TestCropBGRA(t *testing.T) {
	srcW, srcH := 4, 4
	src := make([]byte, srcW*srcH*4)
	at := func(x, y int) int { return (y*srcW + x) * 4 }
	src[at(1, 1)+0] = 0xAB
	src[at(1, 1)+2] = 0xCD

	dst, err := cropBGRA(src, srcW, srcH, 1, 1, 2, 2)
	if err != nil {
		t.Fatalf("cropBGRA: %v", err)
	}
	if len(dst) != 2*2*4 {
		t.Fatalf("dst len = %d, want %d", len(dst), 2*2*4)
	}
	if dst[0] != 0xAB || dst[2] != 0xCD {
		t.Errorf("crop origin pixel = B%02x R%02x, want B AB R CD", dst[0], dst[2])
	}
}

func TestCropBGRARejectsOutOfBounds(t *testing.T) {
	src := make([]byte, 4*4*4)
	if _, err := cropBGRA(src, 4, 4, 3, 3, 4, 4); err == nil {
		t.Error("expected error cropping past the source bounds, got nil")
	}
	if _, err := cropBGRA(src, 4, 4, -1, 0, 2, 2); err == nil {
		t.Error("expected error for negative origin, got nil")
	}
}

func TestIsMostlyBlank(t *testing.T) {
	blank := make([]byte, 64*4)
	if !isMostlyBlank(blank) {
		t.Error("all-black buffer should read as mostly blank")
	}
	bright := make([]byte, 64*4)
	for i := range bright {
		bright[i] = 0xFF
	}
	if isMostlyBlank(bright) {
		t.Error("all-white buffer should not read as mostly blank")
	}
}
