package openai_compatible

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

// Explicit opt-in: only the fixed synthetic input below is transmitted.
func TestLiveChatGPTBrowserProvider(t *testing.T) {
	if os.Getenv("CHATGPT_BROWSER_LIVE") != "1" {
		t.Skip("live browser probe disabled")
	}
	token, err := LoadBrowserToken(os.Getenv("CHATGPT_BRIDGE_TOKEN_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	modes := strings.Split(os.Getenv("CHATGPT_BROWSER_LIVE_EFFORTS"), ",")
	if len(modes) == 1 && modes[0] == "" {
		modes = []string{"none", "medium", "high"}
	}
	rows := []map[string]any{}
	for _, effort := range modes {
		t.Run(effort, func(t *testing.T) {
			client, err := New(Config{BaseURL: "http://127.0.0.1:8791", Provider: "chatgpt_browser", Model: "gpt-6", APIKey: token, ReasoningEffort: effort, BrowserRetries: 1, JSONSchema: true, Timeout: BrowserTimeout(effort), PromptSet: &promptset.Revision{Prompts: map[string]string{"director": "Напиши одну конкретную цель сцены по входным данным. Верни только JSON с единственным непустым строковым полем goal. Не совершай за героиню иных действий."}}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 2*BrowserTimeout(effort)+time.Minute)
			defer cancel()
			response, err := client.Generate(ctx, ai.StoryRequest{Role: "director", PromptVersion: "v1", Input: []byte(`{"action":"Мира осторожно осматривает запертую дверь архива, не открывая её","context":"Дверь остаётся закрытой; Мира не пытается вскрыть замок."}`)})
			row := map[string]any{"effort": effort, "elapsedSeconds": time.Since(start).Seconds(), "protocolRetries": response.ProtocolRetries, "ok": err == nil}
			if err == nil {
				row["output"] = json.RawMessage(response.Output)
			} else {
				row["error"] = err.Error()
			}
			rows = append(rows, row)
			if path := os.Getenv("CHATGPT_BROWSER_LIVE_REPORT"); path != "" {
				raw, _ := json.MarshalIndent(rows, "", "  ")
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("effort=%s ok=%t retries=%d seconds=%.2f", effort, err == nil, response.ProtocolRetries, time.Since(start).Seconds())
			if err != nil {
				t.Error(err)
			}
		})
	}
}
