//go:build windows

package host

import "net/http"

func (s *Server) handleVRStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	out := map[string]any{
		"enabled":   s.pres != nil,
		"vr_seen":   false,
		"vr_ok":     false,
		"vr_detail": "",
	}
	if s.pres != nil {
		seen, ok, detail := s.pres.VRStatus()
		out["vr_seen"] = seen
		out["vr_ok"] = ok
		out["vr_detail"] = detail
	}
	writeJSON(w, out)
}
