//go:build windows

package host

import "net/http"

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/windows", s.handleWindows)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/start", s.handleStart)
	mux.HandleFunc("/api/stop", s.handleStop)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/vr/status", s.handleVRStatus)
	mux.HandleFunc("/api/wt/timings", handleWTTimings)
	mux.HandleFunc("/api/wt/timings/stream", handleWTTimingsStream)
}
