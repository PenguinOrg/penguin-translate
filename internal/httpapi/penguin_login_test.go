package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type fakePenguinAPI struct {
	srv *httptest.Server
}

func newFakePenguinAPI(t *testing.T) *fakePenguinAPI {
	t.Helper()
	f := &fakePenguinAPI{}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/redeem", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Ticket string `json:"ticket"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Ticket != "good-ticket" {
			w.WriteHeader(http.StatusGone)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "pgn_fake_token", "expires_in": 7776000})
	})
	mux.HandleFunc("/v1/provision/openrouter", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer pgn_fake_token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"provider": "openrouter", "key": "sk-or-v1-provisioned", "limit": 5, "created": true})
	})
	mux.HandleFunc("/v1/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer pgn_fake_token" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "bad token"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"discord_id": "42", "username": "tester", "entitlement": "premium",
			"credits": map[string]any{"limit_seconds": 36000, "used_seconds": 5400, "window_days": 30},
		})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func postPenguinLogin(t *testing.T, mux http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/penguin/login", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func deliverCallback(t *testing.T, opened, ticket string) *http.Response {
	t.Helper()
	u, err := url.Parse(opened)
	if err != nil {
		t.Fatal(err)
	}
	port, nonce := u.Query().Get("port"), u.Query().Get("state")
	if port == "" || nonce == "" {
		t.Fatalf("auth url %q missing port/state", opened)
	}
	cb := "http://127.0.0.1:" + port + "/penguin/oauth?ticket=" + url.QueryEscape(ticket) + "&state=" + url.QueryEscape(nonce)
	var resp *http.Response
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err = http.Get(cb)
		if err == nil {
			return resp
		}
		if time.Now().After(deadline) {
			t.Fatalf("loopback listener never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPenguinLoginStoresTokenAndProvisionsOpenRouter(t *testing.T) {
	fake := newFakePenguinAPI(t)
	mux := newTestMux(t)
	postSettings(t, mux, `{"penguin_base_url":"`+fake.srv.URL+`"}`)

	var opened string
	prev := openAuthURL
	openAuthURL = func(url string) error { opened = url; return nil }
	defer func() { openAuthURL = prev }()

	rec := postPenguinLogin(t, mux)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/penguin/login: status %d body %q", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(opened, fake.srv.URL+"/auth/login?") || !strings.Contains(opened, "port=") {
		t.Errorf("browser opened %q, want the loopback auth_url", opened)
	}

	resp := deliverCallback(t, opened, "good-ticket")
	resp.Body.Close()

	deadline := time.Now().Add(10 * time.Second)
	for {
		st := getSettings(t, mux)
		if st["penguin_key_configured"] == true && st["openrouter_key_configured"] == true {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("login flow never stored the keys: penguin=%v openrouter=%v", st["penguin_key_configured"], st["openrouter_key_configured"])
		}
		time.Sleep(50 * time.Millisecond)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/penguin/status", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/penguin/status: status %d body %q", rec.Code, rec.Body.String())
	}
	var status map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status["configured"] != true || status["entitlement"] != "premium" {
		t.Errorf("status = %v, want configured premium", status)
	}
	if status["used_seconds"].(float64) != 5400 || status["limit_seconds"].(float64) != 36000 {
		t.Errorf("credits = %v/%v, want 5400/36000", status["used_seconds"], status["limit_seconds"])
	}
}

func TestPenguinLoginRejectsBadNonce(t *testing.T) {
	fake := newFakePenguinAPI(t)
	mux := newTestMux(t)
	postSettings(t, mux, `{"penguin_base_url":"`+fake.srv.URL+`"}`)

	var opened string
	prev := openAuthURL
	openAuthURL = func(url string) error { opened = url; return nil }
	defer func() { openAuthURL = prev }()

	if rec := postPenguinLogin(t, mux); rec.Code != http.StatusOK {
		t.Fatalf("login: status %d", rec.Code)
	}
	u, _ := url.Parse(opened)
	bad := "http://127.0.0.1:" + u.Query().Get("port") + "/penguin/oauth?ticket=good-ticket&state=wrong-nonce"
	var resp *http.Response
	var err error
	deadline := time.Now().Add(5 * time.Second)
	for {
		if resp, err = http.Get(bad); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("listener never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	resp.Body.Close()
	if st := getSettings(t, mux); st["penguin_key_configured"] == true {
		t.Fatal("a mismatched-nonce callback must not store a token")
	}
}

func TestPenguinLoginCancelReleasesGuard(t *testing.T) {
	fake := newFakePenguinAPI(t)
	mux := newTestMux(t)
	postSettings(t, mux, `{"penguin_base_url":"`+fake.srv.URL+`"}`)

	prev := openAuthURL
	openAuthURL = func(string) error { return nil }
	defer func() { openAuthURL = prev }()

	if rec := postPenguinLogin(t, mux); rec.Code != http.StatusOK {
		t.Fatalf("login: status %d", rec.Code)
	}
	if rec := postPenguinLogin(t, mux); rec.Code != http.StatusConflict {
		t.Fatalf("second login while in flight: status %d, want 409", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/penguin/cancel", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel: status %d", rec.Code)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if r := postPenguinLogin(t, mux); r.Code == http.StatusOK {
			req = httptest.NewRequest(http.MethodPost, "/api/penguin/cancel", nil)
			mux.ServeHTTP(httptest.NewRecorder(), req)
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("guard never released after cancel")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestPenguinLoginRejectsConcurrentFlow(t *testing.T) {
	fake := newFakePenguinAPI(t)
	mux := newTestMux(t)
	postSettings(t, mux, `{"penguin_base_url":"`+fake.srv.URL+`"}`)

	var opened string
	prev := openAuthURL
	openAuthURL = func(u string) error { opened = u; return nil }
	defer func() { openAuthURL = prev }()

	if rec := postPenguinLogin(t, mux); rec.Code != http.StatusOK {
		t.Fatalf("first login: status %d body %q", rec.Code, rec.Body.String())
	}
	if rec := postPenguinLogin(t, mux); rec.Code != http.StatusConflict {
		t.Fatalf("second login while in flight: status %d, want 409", rec.Code)
	}

	deliverCallback(t, opened, "good-ticket").Body.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		rec := postPenguinLogin(t, mux)
		if rec.Code == http.StatusOK {
			deliverCallback(t, opened, "good-ticket").Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("guard never released after completion: status %d", rec.Code)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestPenguinStatusUnconfigured(t *testing.T) {
	mux := newTestMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/penguin/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["configured"] != false {
		t.Fatalf("configured = %v, want false", out["configured"])
	}
}

func TestPenguinStatusUpstreamFailure(t *testing.T) {
	mux := newTestMux(t)
	fake := newFakePenguinAPI(t)
	postSettings(t, mux, `{"penguin_base_url":"`+fake.srv.URL+`","penguin_api_key":"pgn_wrong"}`)
	req := httptest.NewRequest(http.MethodGet, "/api/penguin/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if s, _ := out["error"].(string); s == "" {
		t.Fatal("response is missing an error")
	}
}
