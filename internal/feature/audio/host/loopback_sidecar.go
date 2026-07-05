package host

import "net/http"

// SidecarRoute registers an extra handler on the native local sidecar — the real
// loopback HTTP server (port 8746) that browser WebSockets must connect to,
// because the Wails assetserver cannot upgrade WebSocket connections. Features
// outside the audio package (e.g. the live-translate mic bridge) pass their
// handler in at startup rather than mounting it on the app mux.
type SidecarRoute struct {
	Pattern string
	Handler http.HandlerFunc
}
