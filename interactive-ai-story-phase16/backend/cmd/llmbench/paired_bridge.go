package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func pairedBridgeState(ctx context.Context) (map[string]any, error) {
	token, err := os.ReadFile("../data/secrets/chatgpt-bridge-token")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:8791/status", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("bridge status %d", resp.StatusCode)
	}
	var out map[string]any
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}
func waitPairedWorkers(ctx context.Context) error {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		status, err := pairedBridgeState(ctx)
		if err == nil {
			ready := 0
			if workers, ok := status["workers"].(map[string]any); ok {
				for _, value := range workers {
					w, _ := value.(map[string]any)
					seen, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(w["seen"]))
					if w["status"] == "ready" && (w["version"] == "0.2.2" || w["version"] == "0.3.0" || w["version"] == "0.4.0") && time.Since(seen) < 15*time.Second {
						ready++
					}
				}
			}
			if ready >= 3 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("three ready compatible workers required; no comparison requests sent")
}
func savePairedBridge(ctx context.Context, root, phase string) {
	if status, err := pairedBridgeState(ctx); err == nil {
		_ = savePaired(filepath.Join(root, "bridge-"+phase+".json"), status)
	}
}
