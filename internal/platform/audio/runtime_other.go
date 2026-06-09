//go:build !windows

package audio

const NativeLoopbackPort = ""

func NativeLoopbackBaseURL() string { return "" }

func NativeLoopbackWSURL() string { return "" }
