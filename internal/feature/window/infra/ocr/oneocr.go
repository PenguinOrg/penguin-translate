//go:build windows

package ocr

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

type Engine struct {
	dir    string
	dll    *syscall.DLL
	mu     sync.Mutex
	closed bool

	pipeline   int64
	processOpt int64

	createInitOpts    *syscall.Proc
	initDelayLoad     *syscall.Proc
	createPipeline    *syscall.Proc
	createProcessOpts *syscall.Proc
	setMaxLines       *syscall.Proc
	runPipeline       *syscall.Proc
	getLineCount      *syscall.Proc
	getLine           *syscall.Proc
	getLineContent    *syscall.Proc
	getLineBBox       *syscall.Proc
	getLineWordCount  *syscall.Proc
	getWord           *syscall.Proc
	getWordContent    *syscall.Proc
	getWordBBox       *syscall.Proc
	releaseOcrResult  *syscall.Proc
}

type img struct {
	t       int32
	col     int32
	row     int32
	unk     int32
	step    int64
	dataPtr int64
}

type boundingBox struct {
	X1, Y1, X2, Y2, X3, Y3, X4, Y4 float32
}

const modelKey = `kj)TGtrK>f]b[Piow.gU+nC@s""""""4`

func NewEngine(dir string) (*Engine, error) {
	if ok, err := hasOneOCR(dir); err != nil || !ok {
		return nil, fmt.Errorf("invalid ocr dir %q", dir)
	}
	if err := setDLLDirectory(dir); err != nil {
		return nil, err
	}
	dll, err := syscall.LoadDLL(filepath.Join(dir, "oneocr.dll"))
	if err != nil {
		return nil, fmt.Errorf("load oneocr.dll: %w", err)
	}
	e := &Engine{dir: dir, dll: dll}
	procs := map[string]**syscall.Proc{
		"CreateOcrInitOptions":                        &e.createInitOpts,
		"OcrInitOptionsSetUseModelDelayLoad":          &e.initDelayLoad,
		"CreateOcrPipeline":                           &e.createPipeline,
		"CreateOcrProcessOptions":                     &e.createProcessOpts,
		"OcrProcessOptionsSetMaxRecognitionLineCount": &e.setMaxLines,
		"RunOcrPipeline":                              &e.runPipeline,
		"GetOcrLineCount":                             &e.getLineCount,
		"GetOcrLine":                                  &e.getLine,
		"GetOcrLineContent":                           &e.getLineContent,
		"GetOcrLineBoundingBox":                       &e.getLineBBox,
		"GetOcrLineWordCount":                         &e.getLineWordCount,
		"GetOcrWord":                                  &e.getWord,
		"GetOcrWordContent":                           &e.getWordContent,
		"GetOcrWordBoundingBox":                       &e.getWordBBox,
		"ReleaseOcrResult":                            &e.releaseOcrResult,
	}
	for name, slot := range procs {
		p, err := dll.FindProc(name)
		if err != nil {
			dll.Release()
			return nil, fmt.Errorf("oneocr missing %s: %w", name, err)
		}
		*slot = p
	}
	if err := e.initPipeline(); err != nil {
		dll.Release()
		return nil, err
	}
	return e, nil
}

func setDLLDirectory(dir string) error {
	k32 := syscall.NewLazyDLL("kernel32.dll")
	proc := k32.NewProc("SetDllDirectoryW")
	p16, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	r, _, err := proc.Call(uintptr(unsafe.Pointer(p16)))
	if r == 0 {
		return fmt.Errorf("SetDllDirectory: %w", err)
	}
	return nil
}

func (e *Engine) initPipeline() error {
	var ctx int64
	if r, _, _ := e.createInitOpts.Call(uintptr(unsafe.Pointer(&ctx))); r != 0 {
		return fmt.Errorf("CreateOcrInitOptions: %d", r)
	}
	if r, _, _ := e.initDelayLoad.Call(uintptr(ctx), 0); r != 0 {
		return fmt.Errorf("OcrInitOptionsSetUseModelDelayLoad: %d", r)
	}
	modelPath, _ := syscall.BytePtrFromString(filepath.Join(e.dir, "oneocr.onemodel"))
	keyPtr, _ := syscall.BytePtrFromString(modelKey)
	if r, _, _ := e.createPipeline.Call(
		uintptr(unsafe.Pointer(modelPath)),
		uintptr(unsafe.Pointer(keyPtr)),
		uintptr(ctx),
		uintptr(unsafe.Pointer(&e.pipeline)),
	); r != 0 {
		return fmt.Errorf("CreateOcrPipeline: %d (model in %s)", r, e.dir)
	}
	if r, _, _ := e.createProcessOpts.Call(uintptr(unsafe.Pointer(&e.processOpt))); r != 0 {
		return fmt.Errorf("CreateOcrProcessOptions: %d", r)
	}
	if r, _, _ := e.setMaxLines.Call(uintptr(e.processOpt), 1000); r != 0 {
		return fmt.Errorf("OcrProcessOptionsSetMaxRecognitionLineCount: %d", r)
	}
	return nil
}

func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return
	}
	e.closed = true
	if e.dll != nil {
		e.dll.Release()
		e.dll = nil
	}
}

func (e *Engine) RecognizeResult(pixels []byte, captureW, captureH int) (*Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, fmt.Errorf("ocr engine closed")
	}
	if len(pixels) == 0 || captureW <= 0 || captureH <= 0 {
		return &Result{CaptureW: captureW, CaptureH: captureH}, nil
	}

	ocrPixels, ocrW, ocrH := downscale(pixels, captureW, captureH, 1600)
	sx := float64(captureW) / float64(ocrW)
	sy := float64(captureH) / float64(ocrH)

	buf := make([]byte, len(ocrPixels))
	copy(buf, ocrPixels)

	var pinner runtime.Pinner
	pinner.Pin(&buf[0])
	defer pinner.Unpin()

	im := img{
		t:       3,
		col:     int32(ocrW),
		row:     int32(ocrH),
		step:    int64(ocrW * 4),
		dataPtr: int64(uintptr(unsafe.Pointer(&buf[0]))),
	}

	var instance int64
	if r, _, _ := e.runPipeline.Call(
		uintptr(e.pipeline),
		uintptr(unsafe.Pointer(&im)),
		uintptr(e.processOpt),
		uintptr(unsafe.Pointer(&instance)),
	); r != 0 {
		return nil, fmt.Errorf("RunOcrPipeline: %d", r)
	}
	defer e.releaseOcrResult.Call(uintptr(instance))

	var lineCount int64
	if r, _, _ := e.getLineCount.Call(uintptr(instance), uintptr(unsafe.Pointer(&lineCount))); r != 0 {
		return nil, fmt.Errorf("GetOcrLineCount: %d", r)
	}

	var lines []Line
	var textParts []string
	for i := int64(0); i < lineCount; i++ {
		var lineHandle int64
		if r, _, _ := e.getLine.Call(uintptr(instance), uintptr(i), uintptr(unsafe.Pointer(&lineHandle))); r != 0 || lineHandle == 0 {
			continue
		}
		text := e.readText(lineHandle, e.getLineContent)
		if text == "" {
			continue
		}
		box := e.readBBox(lineHandle, e.getLineBBox)
		box = box.Scale(sx, sy)
		lines = append(lines, Line{Text: text, Box: box})
		textParts = append(textParts, text)
	}

	return &Result{
		Lines:    lines,
		FullText: strings.Join(textParts, "\n"),
		CaptureW: captureW,
		CaptureH: captureH,
	}, nil
}

func (e *Engine) readText(handle int64, proc *syscall.Proc) string {
	var content int64
	if r, _, _ := proc.Call(uintptr(handle), uintptr(unsafe.Pointer(&content))); r != 0 || content == 0 {
		return ""
	}
	return cString(uintptr(content))
}

func (e *Engine) readBBox(handle int64, proc *syscall.Proc) Box {
	var ptr *boundingBox
	if r, _, _ := proc.Call(uintptr(handle), uintptr(unsafe.Pointer(&ptr))); r != 0 || ptr == nil {
		return Box{}
	}
	b := *ptr
	return Box{X1: b.X1, Y1: b.Y1, X2: b.X2, Y2: b.Y2, X3: b.X3, Y3: b.Y3, X4: b.X4, Y4: b.Y4}
}

func downscale(pixels []byte, width, height, maxDim int) ([]byte, int, int) {
	if width <= maxDim && height <= maxDim {
		return pixels, width, height
	}
	scale := float64(maxDim) / float64(max(width, height))
	nw := int(float64(width) * scale)
	nh := int(float64(height) * scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	out := make([]byte, nw*nh*4)
	for y := 0; y < nh; y++ {
		sy := y * height / nh
		for x := 0; x < nw; x++ {
			sx := x * width / nw
			si := (sy*width + sx) * 4
			di := (y*nw + x) * 4
			copy(out[di:di+4], pixels[si:si+4])
		}
	}
	return out, nw, nh
}

func cString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	var n int
	for {
		b := *(*byte)(unsafe.Pointer(ptr + uintptr(n)))
		if b == 0 {
			break
		}
		n++
		if n > 1<<20 {
			break
		}
	}
	if n == 0 {
		return ""
	}
	buf := make([]byte, n)
	copy(buf, (*[1 << 20]byte)(unsafe.Pointer(ptr))[:n:n])
	return string(buf)
}
