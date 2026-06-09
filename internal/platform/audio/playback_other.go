//go:build !windows

package audio

func PlayWAV(path string) error { return nil }

func PlayPCM16LE(samples []byte, sampleRate int) error { return nil }
