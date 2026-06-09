//go:build !denoise_embedded

package denoisebinary

func embedded() bool { return false }

func embeddedBytes() ([]byte, error) {
	return nil, ErrNotEmbedded
}
