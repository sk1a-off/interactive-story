package generation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	secretfile "github.com/local/interactive-ai-story/backend/internal/adapters/secrets/file"
	storyllm "github.com/local/interactive-ai-story/backend/internal/adapters/storyllm/openai_compatible"
	appaiconfig "github.com/local/interactive-ai-story/backend/internal/application/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

type repairProbeLLM struct {
	aiport.StoryLLM
	Calls  int
	Repair bool
	Output string
}

func (l *repairProbeLLM) Generate(ctx context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	l.Calls++
	l.Repair = request.Repair && request.Role == "quest_evaluator"
	response, err := l.StoryLLM.Generate(ctx, request)
	l.Output = string(response.Output)
	return response, err
}

// Explicitly opt-in: ordinary go test never calls a live provider. Inputs are
// the synthetic door fixture and its recorded synthetic Writer output, not Canon.
func TestLiveObjectiveEvidenceRepair(t *testing.T) {
	if os.Getenv("STORY_REPAIR_LIVE") != "1" {
		t.Skip("live synthetic probe is opt-in")
	}
	root := os.Getenv("STORY_REPAIR_ROOT")
	if root == "" {
		t.Fatal("STORY_REPAIR_ROOT required")
	}
	revision, err := id.Parse(os.Getenv("STORY_REPAIR_CONFIG_ID"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := secretfile.NewReadOnly(filepath.Join(root, "data/secrets/provider-keys.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	keys := appaiconfig.CredentialKeys(ctx, store, revision)
	if len(keys) == 0 {
		t.Fatal("no credentials for requested revision")
	}
	client, err := storyllm.New(storyllm.Config{BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", APIKeys: keys, Provider: "google_gemini", Model: "antigravity-preview-05-2026", Profile: "synthetic-repair-probe", Timeout: 5 * time.Minute}, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "tools/local-llm-benchmark/results/durable-mutations-planner-20260907.json"))
	if err != nil {
		t.Fatal(err)
	}
	var archived struct {
		Runs []struct {
			CaseID string `json:"caseId"`
			Text   string `json:"text"`
		} `json:"runs"`
	}
	if err = json.Unmarshal(raw, &archived); err != nil {
		t.Fatal(err)
	}
	beat := ""
	for _, run := range archived.Runs {
		if run.CaseID == "open-door-01" {
			beat = run.Text
		}
	}
	if beat == "" {
		t.Fatal("missing recorded synthetic beat")
	}
	objectiveID := "00000000-0000-4000-8000-000000000201"
	current := []generationtarget.Objective{{ID: objectiveID, Scope: "global", Kind: "quest", QuestType: "main", Title: "Открыть дверь архива", Status: "active", SuccessCriteria: "Дверь архива полностью открыта"}}
	quote := "Алекс распахнул створку до самого упора, пока она с глухим щелчком не зафиксировалась на резиновом ограничителе у плинтуса."
	var report []map[string]any
	for _, tc := range []struct {
		name, beat     string
		wantCompletion bool
	}{
		{"recorded_false_negative", beat, true},
		{"unsupported_completion", "Алекс остался у закрытой двери. Он решил отложить попытку до завтра.", false},
	} {
		llm := &repairProbeLLM{StoryLLM: client}
		rejected := []objectiveChange{{Operation: "complete", ObjectiveID: objectiveID, Progress: 100, EvidenceQuote: quote}}
		if _, err := prepareObjectiveEvidence(tc.beat, current, rejected); err == nil {
			t.Fatal("probe does not trigger evidence rejection")
		}
		start := time.Now()
		changes, repairErr := repairObjectiveEvidence(ctx, llm, tc.beat, current, rejected)
		var events []proposedEvent
		if repairErr == nil {
			events, _, repairErr = validateObjectiveChanges(current, changes)
		}
		completed := false
		for _, event := range events {
			var payload struct {
				ObjectiveID string `json:"objectiveId"`
				Status      string `json:"status"`
				Progress    int    `json:"progress"`
			}
			_ = json.Unmarshal(event.Payload, &payload)
			if event.Type == "objective_updated" && payload.ObjectiveID == objectiveID && payload.Status == "completed" && payload.Progress == 100 {
				completed = true
			}
		}
		passed := repairErr == nil && llm.Calls == 1 && llm.Repair && completed == tc.wantCompletion
		if !tc.wantCompletion {
			passed = passed && len(events) == 0
		}
		errorText := ""
		if repairErr != nil {
			errorText = repairErr.Error()
		}
		report = append(report, map[string]any{"case": tc.name, "passed": passed, "elapsedMs": time.Since(start).Milliseconds(), "calls": llm.Calls, "repair": llm.Repair, "writerCalls": 0, "completed": completed, "output": llm.Output, "error": errorText, "promptSource": "repository embedded quest_evaluator v1 (not active DB revision)", "model": client.Identity().Model})
		t.Logf("case=%s passed=%v calls=%d completed=%v", tc.name, passed, llm.Calls, completed)
		if !passed {
			t.Errorf("live repair probe %s failed", tc.name)
		}
	}
	result, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tools/local-llm-benchmark/results/objective-repair-live-20260907.json"), append(result, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
