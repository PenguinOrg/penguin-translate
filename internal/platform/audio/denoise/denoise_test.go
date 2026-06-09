package denoise

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func buildWAV16k(samples []int16) []byte {
	var b bytes.Buffer
	dataLen := len(samples) * 2
	w32 := func(v uint32) { _ = binary.Write(&b, binary.LittleEndian, v) }
	w16 := func(v uint16) { _ = binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	w32(uint32(36 + dataLen))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	w32(16)
	w16(1)
	w16(1)
	w32(16000)
	w32(16000 * 2)
	w16(2)
	w16(16)
	b.WriteString("data")
	w32(uint32(dataLen))
	for _, s := range samples {
		w16(uint16(s))
	}
	return b.Bytes()
}

func wavRMS(t *testing.T, wav []byte) (rate uint32, rms float64, n int) {
	t.Helper()
	if len(wav) < 44 || string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		t.Fatalf("not a RIFF/WAVE file")
	}
	off := 12
	var data []byte
	for off+8 <= len(wav) {
		id := string(wav[off : off+4])
		sz := int(binary.LittleEndian.Uint32(wav[off+4:]))
		body := off + 8
		if id == "fmt " && sz >= 16 {
			rate = binary.LittleEndian.Uint32(wav[body+4:])
		}
		if id == "data" {
			end := body + sz
			if end > len(wav) {
				end = len(wav)
			}
			data = wav[body:end]
			break
		}
		off = body + sz
	}
	n = len(data) / 2
	if n == 0 {
		return rate, 0, 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		s := int16(binary.LittleEndian.Uint16(data[i*2:]))
		sum += float64(s) * float64(s)
	}
	return rate, math.Sqrt(sum/float64(n)) / 32768.0, n
}

func noise16k(n int) []int16 {
	out := make([]int16, n)
	state := uint32(0x12345678)
	for i := range out {
		state = state*1664525 + 1013904223
		out[i] = int16(state>>16) / 8
	}
	return out
}

func locateSidecar() string {
	for _, p := range []string{
		"../../denoisebinary/penguin-translate-denoise.exe",
		"../../../../runtime/denoise/target/release/penguin-translate-denoise.exe",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func TestDenoiseRoundTrip(t *testing.T) {
	exe := locateSidecar()
	if exe == "" {
		t.Skip("denoise sidecar binary not built; run cargo build in runtime/denoise")
	}
	t.Setenv("TO_DENOISE_EXE", exe)
	t.Cleanup(Stop)

	if !Available() {
		t.Fatalf("Available() = false with TO_DENOISE_EXE=%s", exe)
	}

	in := buildWAV16k(noise16k(16000))
	_, inRMS, _ := wavRMS(t, in)

	out, err := Denoise(in)
	if err != nil {
		t.Fatalf("Denoise: %v", err)
	}
	rate, outRMS, n := wavRMS(t, out)
	if rate != 16000 {
		t.Errorf("output rate = %d, want 16000", rate)
	}
	if n != 16000 {
		t.Errorf("output sample count = %d, want 16000 (length must be preserved)", n)
	}
	if !(outRMS > 0 && outRMS < inRMS*0.95) {
		t.Errorf("expected attenuation: inRMS=%.4f outRMS=%.4f", inRMS, outRMS)
	}
}

func TestDenoiseInvalidInputReturnsOriginal(t *testing.T) {
	out, err := Denoise(nil)
	if err == nil {
		t.Fatalf("expected error for empty input")
	}
	if out != nil {
		t.Errorf("expected original (nil) input echoed back, got %d bytes", len(out))
	}
}

func TestWriteDebugChunk(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TO_DENOISE_DUMP_DIR", dir)

	if got := DebugDir(); got != dir {
		t.Fatalf("DebugDir() = %q, want %q", got, dir)
	}

	raw := buildWAV16k(noise16k(800))
	denoised := buildWAV16k(noise16k(800))
	before := DebugCount()
	WriteDebugChunk(raw, denoised, map[string]any{"denoise_applied": true, "full_text": "テスト"})
	if DebugCount() != before+1 {
		t.Errorf("DebugCount not incremented: before=%d after=%d", before, DebugCount())
	}

	rawFiles, _ := filepath.Glob(filepath.Join(dir, "*-raw.wav"))
	denFiles, _ := filepath.Glob(filepath.Join(dir, "*-denoised.wav"))
	jsonFiles, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(rawFiles) != 1 || len(denFiles) != 1 || len(jsonFiles) != 1 {
		t.Fatalf("expected 1 raw/denoised/json file, got raw=%d denoised=%d json=%d",
			len(rawFiles), len(denFiles), len(jsonFiles))
	}

	b, err := os.ReadFile(jsonFiles[0])
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatalf("meta is not valid JSON: %v", err)
	}
	if meta["full_text"] != "テスト" || meta["denoise_applied"] != true {
		t.Errorf("meta missing fields: %v", meta)
	}
}

func TestWriteDebugChunkDisabledWhenNoDir(t *testing.T) {
	t.Setenv("TO_DENOISE_DUMP_DIR", "")
	t.Setenv("TO_DATA_DIR", "")
	t.Setenv("LOCALAPPDATA", "")
	_ = DebugDir()
	WriteDebugChunk([]byte("x"), nil, nil)
}

func TestDenoiseUnavailableFallsBack(t *testing.T) {
	if Available() {
		t.Skip("sidecar available in this build; unavailable path not reachable")
	}
	in := buildWAV16k(noise16k(800))
	out, err := Denoise(in)
	if err == nil {
		t.Fatalf("expected error when sidecar unavailable")
	}
	if !bytes.Equal(out, in) {
		t.Errorf("expected original audio returned unchanged on fallback")
	}
}
