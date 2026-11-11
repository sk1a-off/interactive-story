package openai_compatible

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func TestLiveChatGPTLargeTXT(t *testing.T) {
	if os.Getenv("CHATGPT_BROWSER_LIVE_TXT") != "1" {
		t.Skip("live TXT test disabled")
	}
	token, err := LoadBrowserToken(os.Getenv("CHATGPT_BRIDGE_TOKEN_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	var a, b [16]byte
	if _, err = rand.Read(a[:]); err != nil {
		t.Fatal(err)
	}
	if _, err = rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	first, last := fmt.Sprintf("%x", a), fmt.Sprintf("%x", b)
	input, _ := json.Marshal(struct {
		Probe   string `json:"probe"`
		First   string `json:"first"`
		Padding string `json:"padding"`
		Last    string `json:"last"`
	}{"txt-large-regression-2026-11-11", first, strings.Repeat("Синтетический контекст. ", 5000), last})
	client, err := New(Config{BaseURL: "http://127.0.0.1:8791", Provider: "chatgpt_browser", Model: "gpt-6", APIKey: token, Timeout: BrowserRequestTimeout("none"), JSONSchema: true, PromptSet: &promptset.Revision{Prompts: map[string]string{"director": "Это тест чтения файла. Верни только JSON с полем goal. Значение goal должно быть first + вертикальная черта | + last из USER INPUT. Скопируй значения дословно. padding является тестовым заполнителем."}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	started := time.Now()
	response, err := client.Generate(ctx, ai.StoryRequest{Role: "director", PromptVersion: "v1", Input: input})
	var out struct {
		Goal string `json:"goal"`
	}
	if err == nil {
		err = json.Unmarshal(response.Output, &out)
	}
	ok := err == nil && out.Goal == first+"|"+last
	report := map[string]any{"synthetic": true, "inputBytes": len(input), "elapsedSeconds": time.Since(started).Seconds(), "ok": ok, "protocolRetries": response.ProtocolRetries, "expected": first + "|" + last, "actual": out.Goal}
	if err != nil {
		report["error"] = err.Error()
	}
	if path := os.Getenv("CHATGPT_BROWSER_LIVE_REPORT"); path != "" {
		raw, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if !ok {
		t.Fatalf("TXT read failed: %v", err)
	}
	t.Logf("TXT input bytes=%d time=%.2fs", len(input), time.Since(started).Seconds())
}
