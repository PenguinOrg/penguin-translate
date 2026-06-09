//go:build overlay_embedded

package overlaybinary

import _ "embed"

//go:embed penguin-translate-overlay.exe
var overlayExe []byte

func embedded() bool { return len(overlayExe) > 0 }

func embeddedBytes() ([]byte, error) {
	if len(overlayExe) == 0 {
		return nil, ErrNotEmbedded
	}
	return overlayExe, nil
}
