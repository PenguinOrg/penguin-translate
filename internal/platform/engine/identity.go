package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const expectedEngineTitle = "translation-overlay-engine"

type engineOpenAPIInfo struct {
	Info struct {
		Title string `json:"title"`
	} `json:"info"`
}

func ProbeEngineIdentity(ctx context.Context, base string, client *http.Client) (ok bool, title string, err error) {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/openapi.json", nil)
	if err != nil {
		return false, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, "", fmt.Errorf("openapi.json status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return false, "", err
	}
	var doc engineOpenAPIInfo
	if err := json.Unmarshal(body, &doc); err != nil {
		return false, "", err
	}
	title = strings.TrimSpace(doc.Info.Title)
	return title == expectedEngineTitle, title, nil
}
