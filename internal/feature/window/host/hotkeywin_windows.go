//go:build windows

package host

import (
	"log"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"translation-overlay/internal/feature/window/infra/hotkey"
)

type HotkeyWindow struct {
	mu       sync.Mutex
	threadID uint32
	mod, vk  uint32
	onToggle func()
	ready    chan struct{}
}

const (
	hotkeyID      = 1
	modNoRepeat   = 0x4000
	wmHotkey      = 0x0312
	wmAppRegister = 0x8000 + 1
	wmAppClear    = 0x8000 + 2
	wmAppQuit     = 0x8000 + 3
)

var (
	user32                = windows.NewLazySystemDLL("user32.dll")
	procRegisterHotKey    = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey  = user32.NewProc("UnregisterHotKey")
	procGetMessageW       = user32.NewProc("GetMessageW")
	procPeekMessageW      = user32.NewProc("PeekMessageW")
	procPostThreadMessage = user32.NewProc("PostThreadMessageW")
	procTranslateMessage  = user32.NewProc("TranslateMessage")
	procDispatchMessageW  = user32.NewProc("DispatchMessageW")
	procGetCurrentThread  = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetCurrentThreadId")
)

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

func NewHotkeyWindow() *HotkeyWindow {
	h := &HotkeyWindow{ready: make(chan struct{})}
	go h.run()
	<-h.ready
	return h
}

func (h *HotkeyWindow) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	tid, _, _ := procGetCurrentThread.Call()
	h.mu.Lock()
	h.threadID = uint32(tid)
	h.mu.Unlock()

	var m msg
	procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, wmAppRegister, wmAppRegister, 0)
	close(h.ready)

	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if ret == 0 || ret == ^uintptr(0) {
			break
		}
		switch m.Message {
		case wmHotkey:
			if m.WParam == hotkeyID {
				h.fire()
			}
		case wmAppRegister:
			h.registerOnThread()
		case wmAppClear:
			procUnregisterHotKey.Call(0, hotkeyID)
		case wmAppQuit:
			procUnregisterHotKey.Call(0, hotkeyID)
			return
		default:
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	}
}

func (h *HotkeyWindow) fire() {
	h.mu.Lock()
	fn := h.onToggle
	h.mu.Unlock()
	if fn != nil {
		log.Print("hotkey pressed — toggling translation/overlay")
		fn()
	}
}

func (h *HotkeyWindow) registerOnThread() {
	h.mu.Lock()
	mod, vk := h.mod, h.vk
	h.mu.Unlock()
	procUnregisterHotKey.Call(0, hotkeyID)
	if vk == 0 {
		return
	}
	r, _, e := procRegisterHotKey.Call(0, hotkeyID, uintptr(mod), uintptr(vk))
	if r == 0 {
		log.Printf("hotkey: RegisterHotKey failed for %s (vk=%d mod=%d): %v",
			hotkey.Format(mod&^modNoRepeat, vk), vk, mod, e)
		return
	}
	log.Printf("hotkey active: %s — toggle translation/overlay while running", hotkey.Format(mod&^modNoRepeat, vk))
}

func (h *HotkeyWindow) post(message uint32) {
	h.mu.Lock()
	tid := h.threadID
	h.mu.Unlock()
	if tid != 0 {
		procPostThreadMessage.Call(uintptr(tid), uintptr(message), 0, 0)
	}
}

func (h *HotkeyWindow) SetHotkey(modifiers, vk uint32, onToggle func()) {
	if onToggle == nil {
		return
	}
	h.mu.Lock()
	h.onToggle = onToggle
	h.mod = modifiers | modNoRepeat
	h.vk = vk
	h.mu.Unlock()
	h.post(wmAppRegister)
}

func (h *HotkeyWindow) ClearHotkey() {
	h.mu.Lock()
	h.vk = 0
	h.mu.Unlock()
	h.post(wmAppClear)
}

func (h *HotkeyWindow) Close() {
	if h == nil {
		return
	}
	h.post(wmAppQuit)
}
