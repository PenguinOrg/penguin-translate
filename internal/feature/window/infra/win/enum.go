//go:build windows

package win

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type Window struct {
	HWND        windows.Handle
	Title       string
	ProcessName string
}

var (
	user32                    = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows           = user32.NewProc("EnumWindows")
	procGetWindowTextLengthW  = user32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW        = user32.NewProc("GetWindowTextW")
	procIsWindowVisible       = user32.NewProc("IsWindowVisible")
	procIsWindow              = user32.NewProc("IsWindow")
	procIsIconic              = user32.NewProc("IsIconic")
	procGetShellWindow        = user32.NewProc("GetShellWindow")
	procGetWindowThreadProcID = user32.NewProc("GetWindowThreadProcessId")
)

func IsWindow(h windows.Handle) bool {
	if h == 0 {
		return false
	}
	r, _, _ := procIsWindow.Call(uintptr(h))
	return r != 0
}

func IsIconic(h windows.Handle) bool {
	if h == 0 {
		return false
	}
	r, _, _ := procIsIconic.Call(uintptr(h))
	return r != 0
}

func FindByProcessName(name string) (Window, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Window{}, false
	}
	ws, err := ListVisible()
	if err != nil {
		return Window{}, false
	}
	for _, w := range ws {
		if w.ProcessName != "" && strings.EqualFold(w.ProcessName, name) {
			return w, true
		}
	}
	return Window{}, false
}

func ListVisible() ([]Window, error) {
	shell, _, _ := procGetShellWindow.Call()
	var out []Window
	cb := syscall.NewCallback(func(hwnd syscall.Handle, _ uintptr) uintptr {
		if uintptr(hwnd) == shell {
			return 1
		}
		vis, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
		if vis == 0 {
			return 1
		}
		title := windowTitle(windows.Handle(hwnd))
		if title == "" {
			return 1
		}
		var pid uint32
		procGetWindowThreadProcID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))
		name := ""
		if pid != 0 {
			if p, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid); err == nil {
				name, _ = processName(p)
				windows.CloseHandle(p)
			}
		}
		out = append(out, Window{
			HWND:        windows.Handle(hwnd),
			Title:       title,
			ProcessName: name,
		})
		return 1
	})
	r, _, err := procEnumWindows.Call(cb, 0)
	if r == 0 && err != syscall.Errno(0) {
		return nil, err
	}
	return out, nil
}

func windowTitle(hwnd windows.Handle) string {
	n, _, _ := procGetWindowTextLengthW.Call(uintptr(hwnd))
	if n == 0 {
		return ""
	}
	buf := make([]uint16, int(n)+1)
	procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf)
}

func processName(h windows.Handle) (string, error) {
	var buf [windows.MAX_PATH]uint16
	if err := windows.GetModuleFileNameEx(h, 0, &buf[0], uint32(len(buf))); err != nil {
		return "", err
	}
	p := windows.UTF16ToString(buf[:])
	p = strings.TrimRight(p, "\x00")
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		p = p[i+1:]
	}
	return p, nil
}

func FormatLabel(w Window, i int) string {
	return fmt.Sprintf("[%d] %s — %s", i, w.Title, w.ProcessName)
}
