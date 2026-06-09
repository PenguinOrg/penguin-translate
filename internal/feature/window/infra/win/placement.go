//go:build windows

package win

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var ErrWindowNotReady = errors.New("window not ready")

type Placement struct {
	ScreenX int
	ScreenY int
	ScreenW int
	ScreenH int
}

func (p Placement) Equal(o Placement) bool {
	return p.ScreenX == o.ScreenX && p.ScreenY == o.ScreenY &&
		p.ScreenW == o.ScreenW && p.ScreenH == o.ScreenH
}

func (p Placement) NearEqual(o Placement, eps int) bool {
	if p.ScreenW != o.ScreenW || p.ScreenH != o.ScreenH {
		return false
	}
	if eps < 0 {
		eps = 0
	}
	return absInt(p.ScreenX-o.ScreenX) <= eps && absInt(p.ScreenY-o.ScreenY) <= eps
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

type windowInfo struct {
	cbSize          uint32
	rcWindow        rect
	rcClient        rect
	dwStyle         uint32
	dwExStyle       uint32
	dwWindowStatus  uint32
	cxWindowBorders uint32
	cyWindowBorders uint32
	atomWindowType  uint16
	wCreatorVersion uint16
}

var (
	procGetWindowInfo = user32.NewProc("GetWindowInfo")
)

func getWindowInfo(hwnd windows.Handle) (windowInfo, error) {
	var wi windowInfo
	wi.cbSize = uint32(unsafe.Sizeof(wi))
	ok, _, _ := procGetWindowInfo.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&wi)))
	if ok == 0 {
		return wi, fmt.Errorf("GetWindowInfo failed")
	}
	return wi, nil
}

func ClientPlacement(hwnd windows.Handle) (Placement, error) {
	wi, err := getWindowInfo(hwnd)
	if err != nil {
		return Placement{}, err
	}
	w := int(wi.rcClient.Right - wi.rcClient.Left)
	h := int(wi.rcClient.Bottom - wi.rcClient.Top)
	if w <= 0 || h <= 0 {
		return Placement{}, fmt.Errorf("%w: invalid client rect", ErrWindowNotReady)
	}
	return Placement{
		ScreenX: int(wi.rcClient.Left),
		ScreenY: int(wi.rcClient.Top),
		ScreenW: w,
		ScreenH: h,
	}, nil
}
