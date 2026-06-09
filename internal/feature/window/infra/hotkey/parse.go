//go:build windows

package hotkey

import (
	"fmt"
	"strings"
)

const (
	ModAlt     = 0x0001
	ModControl = 0x0002
	ModShift   = 0x0004
	ModWin     = 0x0008
)

func Parse(s string) (modifiers uint32, vk uint32, ok bool, err error) {
	s = normalizeHotkeyString(s)
	if s == "" || strings.EqualFold(s, "none") || strings.EqualFold(s, "off") {
		return 0, 0, false, nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '+' || r == '-' || r == ' '
	})
	var mods uint32
	var keyPart string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		switch strings.ToLower(p) {
		case "ctrl", "control":
			mods |= ModControl
		case "alt":
			mods |= ModAlt
		case "shift":
			mods |= ModShift
		case "win", "windows", "super":
			mods |= ModWin
		default:
			if keyPart != "" {
				return 0, 0, false, fmt.Errorf("hotkey: multiple keys in %q", s)
			}
			keyPart = p
		}
	}
	if keyPart == "" {
		return 0, 0, false, fmt.Errorf("hotkey: missing key in %q", s)
	}
	vk, err = nameToVK(keyPart)
	if err != nil {
		return 0, 0, false, err
	}
	return mods, vk, true, nil
}

func normalizeHotkeyString(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '-', ' ':
			b.WriteByte('+')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func nameToVK(name string) (uint32, error) {
	u := strings.ToUpper(name)
	if len(u) == 1 && u[0] >= 'A' && u[0] <= 'Z' {
		return uint32(u[0]), nil
	}
	if len(u) == 1 && u[0] >= '0' && u[0] <= '9' {
		return uint32(u[0]), nil
	}
	if vk, ok := namedKeys[u]; ok {
		return vk, nil
	}
	return 0, fmt.Errorf("hotkey: unknown key %q", name)
}

func Format(modifiers, vk uint32) string {
	var parts []string
	if modifiers&ModAlt != 0 {
		parts = append(parts, "Alt")
	}
	if modifiers&ModControl != 0 {
		parts = append(parts, "Ctrl")
	}
	if modifiers&ModShift != 0 {
		parts = append(parts, "Shift")
	}
	if modifiers&ModWin != 0 {
		parts = append(parts, "Win")
	}
	name := "?"
	for n, code := range namedKeys {
		if code == vk {
			name = n
			break
		}
	}
	if name == "?" && vk >= 'A' && vk <= 'Z' {
		name = string(rune(vk))
	}
	if name == "?" && vk >= '0' && vk <= '9' {
		name = string(rune(vk))
	}
	parts = append(parts, name)
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += "+" + parts[i]
	}
	return out
}

var namedKeys = map[string]uint32{
	"F1": 0x70, "F2": 0x71, "F3": 0x72, "F4": 0x73, "F5": 0x74, "F6": 0x75,
	"F7": 0x76, "F8": 0x77, "F9": 0x78, "F10": 0x79, "F11": 0x7A, "F12": 0x7B,
	"F13": 0x7C, "F14": 0x7D, "F15": 0x7E, "F16": 0x7F, "F17": 0x80, "F18": 0x81,
	"F19": 0x82, "F20": 0x83, "F21": 0x84, "F22": 0x85, "F23": 0x86, "F24": 0x87,
	"SPACE": 0x20, "TAB": 0x09, "ESC": 0x1B, "ESCAPE": 0x1B,
	"INSERT": 0x2D, "DELETE": 0x2E, "HOME": 0x24, "END": 0x23,
	"PAGEUP": 0x21, "PAGEDOWN": 0x22,
}
