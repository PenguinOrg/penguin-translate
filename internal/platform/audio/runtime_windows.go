//go:build windows

package audio

import (
	"net/url"
	"os"
	"strings"
)

const NativeLoopbackPort = "8746"

func NativeLoopbackBaseURL() string {
	host := strings.TrimSpace(os.Getenv("TO_NATIVE_AUDIO_HOST"))
	if host == "" {
		host = "127.0.0.1"
	}
	port := strings.TrimSpace(os.Getenv("TO_NATIVE_AUDIO_PORT"))
	if port == "" {
		port = NativeLoopbackPort
	}
	return "http://" + host + ":" + port
}

func NativeLoopbackWSURL() string {
	base := NativeLoopbackBaseURL()
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = "/ws/loopback"
	return u.String()
}
