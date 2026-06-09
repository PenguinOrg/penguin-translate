//go:build windows

package win

func EnableDpiAwareness() bool {
	const (
		dpiAwarenessContextPerMonitorV2 = ^uintptr(3)
		processPerMonitorDpiAware       = 2
	)
	if proc := user32.NewProc("SetProcessDpiAwarenessContext"); proc.Find() == nil {
		if r, _, _ := proc.Call(dpiAwarenessContextPerMonitorV2); r != 0 {
			return true
		}
	}
	if proc := user32.NewProc("SetProcessDpiAwareness"); proc.Find() == nil {
		if r, _, _ := proc.Call(processPerMonitorDpiAware); r == 0 {
			return true
		}
	}
	if proc := user32.NewProc("SetProcessDPIAware"); proc.Find() == nil {
		if r, _, _ := proc.Call(); r != 0 {
			return true
		}
	}
	return false
}

func EnableDpiAwarenessOnThread() {
	const dpiAwarenessContextPerMonitorV2 = ^uintptr(3)
	proc := user32.NewProc("SetThreadDpiAwarenessContext")
	if proc.Find() != nil {
		return
	}
	_, _, _ = proc.Call(dpiAwarenessContextPerMonitorV2)
}
