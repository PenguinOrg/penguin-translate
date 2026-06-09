package engine

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"
)

func requestEngineShutdown(base string) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/shutdown", nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("engine shutdown: HTTP %s", resp.Status)
		return
	}

	time.Sleep(600 * time.Millisecond)
}
