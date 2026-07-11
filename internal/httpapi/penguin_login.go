package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"translation-overlay/internal/composition"
	"translation-overlay/internal/platform/cloudapi"
	"translation-overlay/internal/platform/domain"
)

// Must match the backend ticket TTL.
const penguinLoginTimeout = 5 * time.Minute

// openAuthURL is replaceable in tests.
var openAuthURL = func(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

var (
	penguinLoginMu       sync.Mutex
	penguinLoginInFlight bool
	penguinCurrentFlow   *penguinLoginFlow
)

// The loopback callback ties sign-in to the initiating machine.
type penguinLoginFlow struct {
	app   *composition.App
	base  string
	nonce string
	srv   *http.Server
	done  chan struct{}
	once  sync.Once
}

func handlePenguinLogin(app *composition.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		st, err := app.SettingsRepo.Load()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		base, err := cloudapi.ResolvePenguinBase(st.PenguinBaseURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		penguinLoginMu.Lock()
		if penguinLoginInFlight {
			penguinLoginMu.Unlock()
			http.Error(w, "a Penguin sign-in is already in progress", http.StatusConflict)
			return
		}
		penguinLoginInFlight = true
		penguinLoginMu.Unlock()

		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			releasePenguinLogin()
			log.Printf("penguin login: create nonce: %v", err)
			http.Error(w, "could not start sign-in", http.StatusInternalServerError)
			return
		}
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			releasePenguinLogin()
			log.Printf("penguin login: open callback listener: %v", err)
			http.Error(w, "could not start sign-in", http.StatusInternalServerError)
			return
		}
		port := ln.Addr().(*net.TCPAddr).Port
		flow := &penguinLoginFlow{
			app:   app,
			base:  base,
			nonce: hex.EncodeToString(nonce),
			done:  make(chan struct{}),
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/penguin/oauth", flow.handleCallback)
		flow.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		penguinLoginMu.Lock()
		penguinCurrentFlow = flow
		penguinLoginMu.Unlock()
		go flow.srv.Serve(ln)
		go flow.reap()

		authURL := fmt.Sprintf("%s/auth/login?port=%d&state=%s", base, port, flow.nonce)
		if err := openAuthURL(authURL); err != nil {
			flow.finish()
			log.Printf("penguin login: open browser: %v", err)
			http.Error(w, "could not open the sign-in page", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "pending"})
	}
}

func (f *penguinLoginFlow) reap() {
	t := time.NewTimer(penguinLoginTimeout)
	defer t.Stop()
	select {
	case <-f.done:
	case <-t.C:
		log.Print("penguin login: timed out waiting for the browser callback")
	}
	_ = f.srv.Shutdown(context.Background())
	releasePenguinLogin()
}

func (f *penguinLoginFlow) finish() { f.once.Do(func() { close(f.done) }) }

func (f *penguinLoginFlow) handleCallback(w http.ResponseWriter, r *http.Request) {
	defer f.finish()
	q := r.URL.Query()
	if !hmac.Equal([]byte(q.Get("state")), []byte(f.nonce)) {
		loopbackPage(w, "Sign-in failed", "Unexpected callback. Start the sign-in again from the app.")
		return
	}
	ticket := q.Get("ticket")
	if ticket == "" {
		loopbackPage(w, "Sign-in failed", "Could not complete sign-in. Start again from the app.")
		return
	}
	token, err := penguinRedeem(f.base, ticket)
	if err != nil {
		log.Printf("penguin login: redeem: %v", err)
		loopbackPage(w, "Sign-in failed", "Could not complete sign-in. Please try again.")
		return
	}
	if _, err := f.app.SettingsRepo.Update(func(st *domain.Settings) error {
		st.PenguinAPIKey = token
		return nil
	}); err != nil {
		log.Printf("penguin login: store token: %v", err)
		loopbackPage(w, "Sign-in failed", "Could not save your sign-in.")
		return
	}
	penguinProvisionOpenRouter(f.app, f.base, token)
	loopbackPage(w, "Signed in", "Sign-in complete. You can close this tab.")
}

func penguinRedeem(base, ticket string) (string, error) {
	body, _ := json.Marshal(map[string]string{"ticket": ticket})
	cl := &http.Client{Timeout: 30 * time.Second}
	resp, err := cl.Post(base+"/auth/redeem", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("Penguin auth/redeem: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Penguin auth/redeem HTTP %d: %s", resp.StatusCode, truncateBody(b))
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", fmt.Errorf("Penguin auth/redeem: bad response: %w", err)
	}
	token := strings.TrimSpace(out.AccessToken)
	if token == "" {
		return "", fmt.Errorf("Penguin auth/redeem: response missing access_token")
	}
	return token, nil
}

func penguinProvisionOpenRouter(app *composition.App, base, token string) {
	req, err := http.NewRequest(http.MethodPost, base+"/v1/provision/openrouter", nil)
	if err != nil {
		log.Printf("penguin login: provision request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	cl := &http.Client{Timeout: 30 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		log.Printf("penguin login: provision openrouter: %v", err)
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		log.Printf("penguin login: provision openrouter HTTP %d: %s", resp.StatusCode, truncateBody(b))
		return
	}
	var out struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(b, &out); err != nil || strings.TrimSpace(out.Key) == "" {
		log.Printf("penguin login: provision response missing key: %s", truncateBody(b))
		return
	}
	if _, err := app.SettingsRepo.Update(func(st *domain.Settings) error {
		st.OpenRouterAPIKey = strings.TrimSpace(out.Key)
		return nil
	}); err != nil {
		log.Printf("penguin login: store provisioned OpenRouter key: %v", err)
	}
}

func handlePenguinStatus(app *composition.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		st, err := app.SettingsRepo.Load()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		key := strings.TrimSpace(st.PenguinAPIKey)
		if key == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{"configured": false})
			return
		}
		base, err := cloudapi.ResolvePenguinBase(st.PenguinBaseURL)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		req, err := http.NewRequest(http.MethodGet, base+"/v1/me", nil)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		req.Header.Set("Authorization", "Bearer "+key)
		cl := &http.Client{Timeout: 15 * time.Second}
		resp, err := cl.Do(req)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode != http.StatusOK {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": fmt.Sprintf("Penguin /v1/me HTTP %d: %s", resp.StatusCode, truncateBody(b))})
			return
		}
		var me struct {
			DiscordID   string `json:"discord_id"`
			Username    string `json:"username"`
			DisplayName string `json:"display_name"`
			AvatarURL   string `json:"avatar_url"`
			Entitlement string `json:"entitlement"`
			Credits     struct {
				LimitSeconds float64 `json:"limit_seconds"`
				UsedSeconds  float64 `json:"used_seconds"`
			} `json:"credits"`
		}
		if err := json.Unmarshal(b, &me); err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "Penguin /v1/me: bad response: " + err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"configured":    true,
			"discord_id":    me.DiscordID,
			"username":      me.Username,
			"display_name":  me.DisplayName,
			"avatar_url":    me.AvatarURL,
			"entitlement":   me.Entitlement,
			"used_seconds":  me.Credits.UsedSeconds,
			"limit_seconds": me.Credits.LimitSeconds,
		})
	}
}

func handlePenguinCancel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		penguinLoginMu.Lock()
		flow := penguinCurrentFlow
		penguinLoginMu.Unlock()
		if flow != nil {
			flow.finish()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"cancelled": flow != nil})
	}
}

func releasePenguinLogin() {
	penguinLoginMu.Lock()
	penguinLoginInFlight = false
	penguinCurrentFlow = nil
	penguinLoginMu.Unlock()
}

func loopbackPage(w http.ResponseWriter, title, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>Penguin Translate</title>`+
		`<div style="font-family:system-ui,sans-serif;text-align:center;margin-top:20vh">`+
		`<h1 style="font-size:1.4rem">%s</h1><p style="color:#666">%s</p></div>`, title, msg)
}

func truncateBody(b []byte) string {
	s := string(b)
	if len(s) > 600 {
		return s[:600] + "…"
	}
	return s
}
