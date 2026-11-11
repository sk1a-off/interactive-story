package openai_compatible

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

// Opt-in and fixed synthetic prompts only; this test never submits a story action.
func TestLiveChatGPTBrowserParallelQueue(t *testing.T) {
	if os.Getenv("CHATGPT_BROWSER_LIVE_PARALLEL") != "1" {
		t.Skip("live parallel browser probe disabled")
	}
	token, err := LoadBrowserToken(os.Getenv("CHATGPT_BRIDGE_TOKEN_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{BaseURL: "http://127.0.0.1:8791", Provider: "chatgpt_browser", Model: "gpt-6", APIKey: token, Timeout: BrowserRequestTimeout("none"), JSONSchema: true, PromptSet: &promptset.Revision{Prompts: map[string]string{"director": "Это синтетический тест очереди. Верни только JSON с единственным полем goal. Скопируй значение probe из входа в goal дословно. Другой текст не добавляй."}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	rows := make([]map[string]any, 6)
	started := time.Now()
	var wg sync.WaitGroup
	for i := range rows {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			probe := fmt.Sprintf("browser-parallel-live-2026-11-11-%d", index+1)
			input, _ := json.Marshal(map[string]string{"probe": probe, "action": "Мира осматривает дверь, не открывая её."})
			response, callErr := client.Generate(ctx, ai.StoryRequest{Role: "director", PromptVersion: "v1", Input: input})
			row := map[string]any{"probe": probe, "elapsedSeconds": time.Since(started).Seconds(), "ok": callErr == nil, "protocolRetries": response.ProtocolRetries}
			if callErr != nil {
				row["error"] = callErr.Error()
				t.Error(callErr)
			} else {
				var output struct {
					Goal string `json:"goal"`
				}
				_ = json.Unmarshal(response.Output, &output)
				row["goal"] = output.Goal
				if output.Goal != probe {
					row["ok"] = false
					t.Errorf("response correlation: got %q want %q", output.Goal, probe)
				}
			}
			rows[index] = row
		}(i)
	}
	wg.Wait()
	if path := os.Getenv("CHATGPT_BROWSER_LIVE_REPORT"); path != "" {
		raw, _ := json.MarshalIndent(rows, "", "  ")
		if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("six queued requests completed in %.2fs", time.Since(started).Seconds())
}
