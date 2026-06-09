//go:build windows

package win

func FrameHash(pixels []byte, width, height int) uint64 {
	if len(pixels) == 0 || width <= 0 || height <= 0 {
		return 0
	}
	stride := width * 4
	var h uint64 = uint64(width) ^ uint64(height)<<17
	step := stride / 64
	if step < 4 {
		step = 4
	}
	for y := 0; y < height; y += 8 {
		off := y*stride + (y%8)*4
		if off+3 < len(pixels) {
			h = h*31 + uint64(pixels[off]) + uint64(pixels[off+1])<<8 + uint64(pixels[off+2])<<16
		}
	}
	for i := 0; i < len(pixels)-3; i += step {
		h = h*31 + uint64(pixels[i]) + uint64(pixels[i+2])<<16
	}
	return h
}
