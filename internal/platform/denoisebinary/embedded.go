//go:build denoise_embedded

package denoisebinary

import _ "embed"

//go:embed penguin-translate-denoise.exe
var denoiseExe []byte

func embedded() bool { return len(denoiseExe) > 0 }

func embeddedBytes() ([]byte, error) {
	if len(denoiseExe) == 0 {
		return nil, ErrNotEmbedded
	}
	return denoiseExe, nil
}
