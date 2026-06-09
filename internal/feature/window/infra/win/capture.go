//go:build windows

package win

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	gdi32                      = windows.NewLazySystemDLL("gdi32.dll")
	procGetWindowDC            = user32.NewProc("GetWindowDC")
	procReleaseDC              = user32.NewProc("ReleaseDC")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procPrintWindow            = user32.NewProc("PrintWindow")
	procGetDIBits              = gdi32.NewProc("GetDIBits")
)

const (
	pwRenderFullContent = 0x2
)

type bitmapInfoHeader struct {
	Size, Width, Height          int32
	Planes, BitCount             uint16
	Compression, SizeImage       uint32
	XPelsPerMeter, YPelsPerMeter int32
	ClrUsed, ClrImportant        uint32
}

type Frame struct {
	Pixels    []byte
	Width     int
	Height    int
	Placement Placement
}

func CaptureHWND(hwnd windows.Handle) (Frame, error) {
	var empty Frame
	wi, err := getWindowInfo(hwnd)
	if err != nil {
		return empty, err
	}
	place, err := ClientPlacement(hwnd)
	if err != nil {
		return empty, err
	}

	winW := int(wi.rcWindow.Right - wi.rcWindow.Left)
	winH := int(wi.rcWindow.Bottom - wi.rcWindow.Top)
	cropX := int(wi.rcClient.Left - wi.rcWindow.Left)
	cropY := int(wi.rcClient.Top - wi.rcWindow.Top)
	clientW, clientH := place.ScreenW, place.ScreenH

	if winW <= 0 || winH <= 0 {
		return empty, fmt.Errorf("%w: invalid window rect", ErrWindowNotReady)
	}

	full, err := capturePrintWindow(hwnd, winW, winH)
	if err != nil {
		return empty, err
	}
	pixels, err := cropBGRA(full, winW, winH, cropX, cropY, clientW, clientH)
	if err != nil {
		return empty, err
	}
	if isMostlyBlank(pixels) {
		return empty, fmt.Errorf(
			"capture empty — use borderless/windowed (not exclusive fullscreen), keep VRChat visible",
		)
	}

	return Frame{
		Pixels: pixels, Width: clientW, Height: clientH,
		Placement: place,
	}, nil
}

func cropBGRA(src []byte, srcW, srcH, x, y, dstW, dstH int) ([]byte, error) {
	if x < 0 || y < 0 || dstW <= 0 || dstH <= 0 {
		return nil, fmt.Errorf("invalid crop rect")
	}
	if x+dstW > srcW || y+dstH > srcH {
		return nil, fmt.Errorf("crop outside capture (%dx%d in %dx%d at %d,%d)", dstW, dstH, srcW, srcH, x, y)
	}
	srcStride := srcW * 4
	dstStride := dstW * 4
	dst := make([]byte, dstStride*dstH)
	for row := 0; row < dstH; row++ {
		srcOff := (y+row)*srcStride + x*4
		dstOff := row * dstStride
		copy(dst[dstOff:dstOff+dstStride], src[srcOff:srcOff+dstStride])
	}
	return dst, nil
}

func capturePrintWindow(hwnd windows.Handle, width, height int) ([]byte, error) {
	hdcSrc, _, _ := procGetWindowDC.Call(uintptr(hwnd))
	if hdcSrc == 0 {
		return nil, fmt.Errorf("GetWindowDC failed")
	}
	defer procReleaseDC.Call(uintptr(hwnd), hdcSrc)

	hdcMem, _, _ := procCreateCompatibleDC.Call(hdcSrc)
	if hdcMem == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(hdcMem)

	hbmp, _, _ := procCreateCompatibleBitmap.Call(hdcSrc, uintptr(width), uintptr(height))
	if hbmp == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer procDeleteObject.Call(hbmp)

	old, _, _ := procSelectObject.Call(hdcMem, hbmp)
	ret, _, _ := procPrintWindow.Call(uintptr(hwnd), hdcMem, pwRenderFullContent)
	procSelectObject.Call(hdcMem, old)
	if ret == 0 {
		old, _, _ = procSelectObject.Call(hdcMem, hbmp)
		procPrintWindow.Call(uintptr(hwnd), hdcMem, 0)
		procSelectObject.Call(hdcMem, old)
	}

	return readBitmapBits(hdcMem, hbmp, width, height)
}

func readBitmapBits(hdcMem, hbmp uintptr, width, height int) ([]byte, error) {
	bi := bitmapInfoHeader{
		Size:        40,
		Width:       int32(width),
		Height:      -int32(height),
		Planes:      1,
		BitCount:    32,
		Compression: 0,
	}
	stride := width * 4
	pixels := make([]byte, stride*height)
	ret, _, _ := procGetDIBits.Call(
		hdcMem, hbmp, 0, uintptr(height),
		uintptr(unsafe.Pointer(&pixels[0])),
		uintptr(unsafe.Pointer(&bi)),
		0,
	)
	if ret == 0 {
		return nil, fmt.Errorf("GetDIBits failed")
	}
	return pixels, nil
}

func isMostlyBlank(pixels []byte) bool {
	if len(pixels) < 64 {
		return true
	}
	step := len(pixels) / 64
	if step < 4 {
		step = 4
	}
	var bright int
	var samples int
	for i := 0; i < len(pixels)-3; i += step {
		b := int(pixels[i])
		g := int(pixels[i+1])
		r := int(pixels[i+2])
		bright += r + g + b
		samples++
	}
	if samples == 0 {
		return true
	}
	return bright/samples < 18
}
