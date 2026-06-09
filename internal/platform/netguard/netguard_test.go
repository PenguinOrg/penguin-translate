package netguard

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsLoopbackHost(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8746":    true,
		"127.0.0.1":         true,
		"127.5.6.7:80":      true,
		"localhost":         true,
		"localhost:8745":    true,
		"wails.localhost":   true,
		"[::1]:8746":        true,
		"[::1]":             true,
		"::1":               true,
		"evil.com":          false,
		"evil.com:8746":     false,
		"sub.evil.com":      false,
		"10.0.0.5:8746":     false,
		"192.168.1.10":      false,
		"169.254.1.1":       false,
		"localhost.evil.io": false,
		"":                  false,
	}
	for host, want := range cases {
		if got := IsLoopbackHost(host); got != want {
			t.Errorf("IsLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestAllowBrowserOrigin(t *testing.T) {
	cases := map[string]bool{
		"":                        true,
		"http://127.0.0.1:8745":   true,
		"http://localhost:8745":   true,
		"http://wails.localhost":  true,
		"https://wails.localhost": true,
		"http://[::1]:8745":       true,
		"https://evil.com":        false,
		"http://evil.com:8746":    false,
		"null":                    false,
	}
	for origin, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/ws/loopback", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if got := AllowBrowserOrigin(r); got != want {
			t.Errorf("AllowBrowserOrigin(Origin=%q) = %v, want %v", origin, got, want)
		}
	}
}

func TestRequireLoopbackHost(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	guard := RequireLoopbackHost(next)

	cases := map[string]int{
		"127.0.0.1:8746":  http.StatusOK,
		"localhost:8745":  http.StatusOK,
		"wails.localhost": http.StatusOK,
		"evil.com:8746":   http.StatusForbidden,
		"evil.com":        http.StatusForbidden,
	}
	for host, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
		r.Host = host
		w := httptest.NewRecorder()
		guard.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("RequireLoopbackHost(Host=%q) = %d, want %d", host, w.Code, want)
		}
	}
}
