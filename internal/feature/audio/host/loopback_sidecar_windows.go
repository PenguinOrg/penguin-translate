//go:build windows

package host

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	audiosys "translation-overlay/internal/platform/audio"
	"translation-overlay/internal/platform/netguard"
)

var sidecarOnce sync.Once

func StartNativeLoopbackSidecar(ctx context.Context) {
	sidecarOnce.Do(func() {
		go serveLoopbackSidecar(ctx)
	})
}

func serveLoopbackSidecar(ctx context.Context) {
	base := audiosys.NativeLoopbackBaseURL()
	u, err := url.Parse(base)
	if err != nil {
		log.Printf("loopback sidecar: invalid base URL %q: %v", base, err)
		return
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = audiosys.NativeLoopbackPort
	}
	addr := net.JoinHostPort(host, port)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":"true","loopback":"native","port":"` + port + `"}`))
	})
	mux.HandleFunc("/ws/loopback", handleNativeLoopbackWS)

	srv := &http.Server{
		Addr:              addr,
		Handler:           netguard.RequireLoopbackHost(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("native audio sidecar listening on %s (ws /ws/loopback)", base)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("loopback sidecar: %v", err)
		}
	}()

	<-ctx.Done()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
}
