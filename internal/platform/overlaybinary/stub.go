//go:build !overlay_embedded

package overlaybinary

func embedded() bool { return false }

func embeddedBytes() ([]byte, error) {
	return nil, ErrNotEmbedded
}
