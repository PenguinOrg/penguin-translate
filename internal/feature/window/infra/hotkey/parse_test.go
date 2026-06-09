//go:build windows

package hotkey

import "testing"

func TestParseCtrlFVariants(t *testing.T) {
	for _, in := range []string{"Ctrl+F", "ctrl+f", "Ctrl-F", "ctrl-f", "Control + F"} {
		mods, vk, ok, err := Parse(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if !ok {
			t.Fatalf("%q: not ok", in)
		}
		if mods&ModControl == 0 {
			t.Fatalf("%q: missing Ctrl modifier (%d)", in, mods)
		}
		if vk != 'F' {
			t.Fatalf("%q: vk=%d want F", in, vk)
		}
	}
}

func TestParseF9(t *testing.T) {
	mods, vk, ok, err := Parse("F9")
	if err != nil || !ok {
		t.Fatalf("F9: ok=%v err=%v", ok, err)
	}
	if mods != 0 || vk != 0x78 {
		t.Fatalf("F9: mods=%d vk=%d", mods, vk)
	}
}
