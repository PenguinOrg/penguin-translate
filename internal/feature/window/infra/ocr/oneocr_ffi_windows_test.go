//go:build windows

package ocr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOneOCRFFIAgainstFakeDLL(t *testing.T) {
	dll := fakeOneOCRDLL(t)

	dir := t.TempDir()
	if err := copyFile(dll, filepath.Join(dir, "oneocr.dll")); err != nil {
		t.Fatalf("copy fake dll: %v", err)
	}
	recPath := filepath.Join(dir, "ocr-rec.json")
	t.Setenv("WT_FAKE_OCR_OUT", recPath)

	eng, err := NewEngine(dir)
	if err != nil {
		t.Fatalf("NewEngine against fake dll: %v", err)
	}
	defer eng.Close()

	const capW, capH = 3200, 1800
	pixels := make([]byte, capW*capH*4)
	for i := range pixels {
		pixels[i] = 0x40
	}
	res, err := eng.RecognizeResult(pixels, capW, capH)
	if err != nil {
		t.Fatalf("RecognizeResult: %v", err)
	}

	if len(res.Lines) != 2 {
		t.Fatalf("want 2 lines from fake, got %d (%+v)", len(res.Lines), res.Lines)
	}
	if res.Lines[0].Text != "これは" || res.Lines[1].Text != "テスト" {
		t.Errorf("line text round-trip wrong: %q / %q", res.Lines[0].Text, res.Lines[1].Text)
	}
	if res.FullText != "これは\nテスト" {
		t.Errorf("FullText = %q", res.FullText)
	}
	if got := res.Lines[0].Box.X1; got != 20 {
		t.Errorf("bbox not scaled back to capture space: X1=%v want 20", got)
	}
	if got := res.Lines[0].Box.Y3; got != 68 {
		t.Errorf("bbox Y3=%v want 68", got)
	}

	var rec struct {
		ImgT       int32  `json:"img_t"`
		Col        int32  `json:"col"`
		Row        int32  `json:"row"`
		Step       int64  `json:"step"`
		ByteSum    uint64 `json:"byte_sum"`
		MaxLines   int64  `json:"max_lines"`
		ModelKeyOK int    `json:"model_key_ok"`
		RunCount   int    `json:"run_count"`
	}
	raw, err := os.ReadFile(recPath)
	if err != nil {
		t.Fatalf("read fake record: %v", err)
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &rec); err != nil {
		t.Fatalf("parse fake record %q: %v", raw, err)
	}
	if rec.ImgT != 3 {
		t.Errorf("img.t reached the fake as %d, want 3", rec.ImgT)
	}
	if rec.Col != 1600 || rec.Row != 900 {
		t.Errorf("downscaled image dims at the boundary = %dx%d, want 1600x900", rec.Col, rec.Row)
	}
	if rec.Step != 1600*4 {
		t.Errorf("img.step = %d, want %d", rec.Step, 1600*4)
	}
	if rec.ByteSum == 0 {
		t.Errorf("pixel buffer reached the fake empty (byte_sum 0) — data pointer not marshaled")
	}
	if rec.MaxLines != 1000 {
		t.Errorf("max recognition lines = %d, want 1000", rec.MaxLines)
	}
	if rec.ModelKeyOK != 1 {
		t.Errorf("model decryption key did not arrive intact across the boundary")
	}
	if rec.RunCount < 1 {
		t.Errorf("RunOcrPipeline was not invoked")
	}
}

func fakeOneOCRDLL(t *testing.T) string {
	t.Helper()
	if p := strings.TrimSpace(os.Getenv("TO_FAKE_ONEOCR_DLL")); p != "" {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("TO_FAKE_ONEOCR_DLL set but missing: %s", p)
		}
		return p
	}
	local := filepath.Join("testdata", "fake-oneocr", "target", "release", "oneocr.dll")
	if _, err := os.Stat(local); err == nil {
		abs, _ := filepath.Abs(local)
		return abs
	}
	t.Skip("fake oneocr.dll not built — set TO_FAKE_ONEOCR_DLL or `cargo build --release` in testdata/fake-oneocr")
	return ""
}
