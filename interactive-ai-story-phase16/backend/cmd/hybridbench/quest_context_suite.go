package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	pg "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	secretfile "github.com/local/interactive-ai-story/backend/internal/adapters/secrets/file"
	storyllm "github.com/local/interactive-ai-story/backend/internal/adapters/storyllm/openai_compatible"
	appaiconfig "github.com/local/interactive-ai-story/backend/internal/application/aiconfig"
	appgen "github.com/local/interactive-ai-story/backend/internal/application/generation"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

type questFixture struct {
	ID                 string            `json:"id"`
	Input              json.RawMessage   `json:"input"`
	ExpectProgress     bool              `json:"expectProgress"`
	ExpectedOperations map[string]string `json:"expectedOperations,omitempty"`
}

func richQuestFixtures() []questFixture {
	var history []map[string]any
	var cast []map[string]any
	var visuals []map[string]any
	for i, place := range []string{"порт", "мельница", "кузница", "архив", "площадь", "перевал", "сад", "храм", "мост", "мастерская", "башня", "трактир"} {
		history = append(history, map[string]any{"chapterNumber": i + 1, "text": fmt.Sprintf("Глава %d. Алекс посетил %s во время прежнего путешествия. Он изучил расположение улиц, услышал звон колокола и разговоры прохожих. Местный смотритель рассказал о восстановлении старой кровли и устройстве водостоков. После дождя площадь опустела; торговцы убрали прилавки под навесы. Алекс дождался утра и продолжил дорогу. Эти события произошли до появления нынешних повозок; никаких работ по их закреплению здесь не было.", i+1, place)})
		cast = append(cast, map[string]any{"name": fmt.Sprintf("Смотритель %d", i+1), "description": fmt.Sprintf("Житель места %s. Носит серый плащ, чинит кровлю, собирает местные предания. Его заботит сохранность старых зданий. Он остался в своём городе и не участвует в текущей сцене с повозками.", place)})
		visuals = append(visuals, map[string]any{"place": place, "description": "Muted stone walls, weathered copper roofs, diffuse daylight after rain, restrained earth tones, natural cloth textures, eye-level composition. These visual directions define illustration style only; they introduce no story events, possessions, achievements or quest progress."})
	}
	out := []questFixture{}
	for _, tc := range []struct {
		id, action, beat, previous string
		progress                   int
		expect                     bool
	}{
		{"partial", "Закрепить только первую повозку.", "Алекс закрепил первую повозку канатами. Первая повозка надёжно закреплена. Вторая повозка остаётся незакреплённой; Алекс её не трогал.", "Обе повозки незакреплены, канаты лежат рядом.", 0, true},
		{"remembered", "Только вспомнить вчерашнюю работу, ничего не делать.", "Алекс вспомнил вчерашнюю работу и остался на скамье. Сегодня он ничего не закреплял. Состояние обеих повозок не изменилось.", "Первая повозка закреплена вчера, это уже учтено как 50 процентов. Вторая незакреплена.", 50, false},
		{"failed-attempt", "Попытаться закрепить вторую повозку.", "Алекс потянул канат, но тот выскользнул из рук. Вторую повозку закрепить не удалось. Первая по-прежнему закреплена, новых результатов нет.", "Первая повозка закреплена, это уже учтено как 50 процентов. Вторая незакреплена.", 50, false},
	} {
		objectives := []generationtarget.Objective{{ID: "00000000-0000-4000-8000-000000000201", Scope: "global", Kind: "quest", QuestType: "main", Title: "Закрепить обе повозки", SuccessCriteria: "Обе повозки надёжно закреплены канатами.", Status: "active", Progress: tc.progress}}
		input := map[string]any{"action": tc.action, "intent": tc.action, "directorGoal": "Отразить только фактический результат попытки", "newBeat": tc.beat, "currentObjectives": objectives, "context": map[string]any{"previousBeatText": tc.previous, "storyBible": map[string]string{"constraint": "Попытка не гарантирует успех. Не начислять прогресс за воспоминание или неудачу."}, "player": map[string]string{"name": "Алекс"}, "heroJournal": []map[string]any{{"name": "Канат", "quantity": 2, "status": "active"}}, "worldRules": []string{"Закрепление должно быть реально выполнено."}, "worldResources": []any{}, "recentBeats": history, "initialCast": cast, "visualBible": visuals}}
		raw, _ := json.Marshal(input)
		out = append(out, questFixture{ID: tc.id, Input: raw, ExpectProgress: tc.expect})
	}
	return out
}

func checkQuestFixture(f questFixture, output json.RawMessage) error {
	var input struct {
		Beat       string                       `json:"newBeat"`
		Objectives []generationtarget.Objective `json:"currentObjectives"`
	}
	if err := json.Unmarshal(f.Input, &input); err != nil {
		return err
	}
	raw, err := appgen.CheckObjectiveProposal(input.Beat, input.Objectives, output)
	if err != nil {
		return err
	}
	var changes []struct {
		Operation string `json:"operation"`
		ID        string `json:"objectiveId"`
		Progress  int    `json:"progress"`
		Status    string `json:"status"`
	}
	if err = json.Unmarshal(raw, &changes); err != nil {
		return err
	}
	if len(f.ExpectedOperations) > 0 {
		seen := map[string]bool{}
		for _, c := range changes {
			if f.ExpectedOperations[c.ID] != c.Operation || seen[c.ID] {
				return fmt.Errorf("unexpected terminal change")
			}
			if c.Operation == "complete" && (c.Status != "completed" || c.Progress != 100) {
				return fmt.Errorf("invalid completed state")
			}
			if c.Operation == "fail" && c.Status != "failed" {
				return fmt.Errorf("invalid failed state")
			}
			seen[c.ID] = true
		}
		if len(seen) != len(f.ExpectedOperations) {
			return fmt.Errorf("missing expected terminal change")
		}
		return nil
	}
	found := false
	for _, c := range changes {
		if c.Operation == "complete" || c.Operation == "fail" {
			return fmt.Errorf("false terminal outcome")
		}
		if c.ID == input.Objectives[0].ID && c.Operation == "progress" {
			if !f.ExpectProgress || c.Progress <= input.Objectives[0].Progress || c.Progress >= 100 || c.Status != "active" {
				return fmt.Errorf("incorrect progress")
			}
			found = true
		}
		if !f.ExpectProgress {
			return fmt.Errorf("unexpected mutation on no-progress fixture")
		}
	}
	if f.ExpectProgress && !found {
		return fmt.Errorf("missing partial parent progress")
	}
	return nil
}

type questSuiteCall struct {
	CaseID          string          `json:"caseId"`
	Variant         string          `json:"variant"`
	Repeat          int             `json:"repeat"`
	Position        int             `json:"position"`
	InputBytes      int             `json:"inputBytes"`
	InputSHA256     string          `json:"inputSha256"`
	LatencyMS       int64           `json:"latencyMs"`
	InputTokens     int64           `json:"inputTokens"`
	Output          json.RawMessage `json:"output,omitempty"`
	Error           string          `json:"error,omitempty"`
	ValidationError string          `json:"validationError,omitempty"`
	FallbackModel   string          `json:"fallbackModel,omitempty"`
	FallbackLatency int64           `json:"fallbackLatencyMs,omitempty"`
	FallbackOutput  json.RawMessage `json:"fallbackOutput,omitempty"`
	FallbackError   string          `json:"fallbackError,omitempty"`
	FallbackInvalid string          `json:"fallbackValidationError,omitempty"`
	Recovered       bool            `json:"recovered,omitempty"`
}

func runQuestContextSuite(dsn, secretPath, endpoint, model, output string, live, ledgerMode, modelScreen bool, questPromptPath string, repeats int, continueOnError bool, fallbackModel string) error {
	if output == "" {
		return fmt.Errorf("-output is required")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		return fmt.Errorf("output already exists or inaccessible")
	}
	fixtures := richQuestFixtures()
	if modelScreen {
		fixtures = append(fixtures, historyIdentityFixtures()...)
		fixtures = append(fixtures, terminalQuestFixtures()...)
	}
	if ledgerMode {
		fixtures = historyIdentityFixtures()
	}
	candidate := func(f questFixture) (json.RawMessage, error) {
		if modelScreen {
			return f.Input, nil
		}
		if ledgerMode {
			return ledgerSuiteInput(f)
		}
		return compactQuestInput(f.Input)
	}
	report := struct {
		Mode                string               `json:"mode"`
		Model               string               `json:"model"`
		PromptRevision      int64                `json:"promptRevision"`
		QuestPromptOverride *questPromptOverride `json:"questPromptOverride,omitempty"`
		FallbackModel       string               `json:"fallbackModel,omitempty"`
		Fixtures            []questFixture       `json:"fixtures"`
		Calls               []questSuiteCall     `json:"calls"`
	}{Mode: "offline", Model: model, FallbackModel: fallbackModel, Fixtures: fixtures}
	save := func() error {
		raw, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(output), 0755); err != nil {
			return err
		}
		return os.WriteFile(output, raw, 0600)
	}
	for _, f := range fixtures {
		compact, err := candidate(f)
		if err != nil {
			return err
		}
		fmt.Printf("case=%s baseline_bytes=%d compact_bytes=%d reduction=%.1f%%\n", f.ID, len(f.Input), len(compact), 100*(1-float64(len(compact))/float64(len(f.Input))))
	}
	if !live {
		return save()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	pool, err := bootstrap.OpenPostgres(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	active, err := pg.NewAIConfigRepository(pool).Active(ctx)
	if err != nil {
		return err
	}
	store, err := secretfile.NewReadOnly(secretPath)
	if err != nil {
		return err
	}
	keys := appaiconfig.CredentialKeys(ctx, store, active.ID)
	if len(keys) == 0 {
		return fmt.Errorf("no configured keys")
	}
	prompts, err := pg.NewPromptSetRepository(pool).Active(ctx)
	if err != nil {
		return err
	}
	report.QuestPromptOverride, err = overrideQuestPrompt(&prompts, questPromptPath)
	if err != nil {
		return err
	}
	client, err := storyllm.New(storyllm.Config{BaseURL: endpoint, APIKeys: keys, Provider: "google_gemini", Model: model, Profile: "quest-context-suite", Timeout: 5 * time.Minute, PromptSet: &prompts}, nil)
	if err != nil {
		return err
	}
	var fallback ai.StoryLLM
	if fallbackModel != "" {
		fallback, err = storyllm.New(storyllm.Config{BaseURL: endpoint, APIKeys: keys, Provider: "google_gemini", Model: fallbackModel, Profile: "quest-context-suite-fallback", Timeout: 5 * time.Minute, PromptSet: &prompts}, nil)
		if err != nil {
			return err
		}
	}
	report.Mode = "live-balanced"
	if modelScreen {
		report.Mode = "live-full-context-model-screen"
	}
	if ledgerMode {
		report.Mode = "live-balanced-ledger"
	}
	report.PromptRevision = prompts.Revision
	for repeat := 0; repeat < repeats; repeat++ {
		for _, f := range fixtures {
			positions := 2
			if modelScreen {
				positions = 1
			}
			for position := 0; position < positions; position++ {
				variant := "baseline"
				raw := f.Input
				if !modelScreen && (repeat+position)%2 == 1 {
					variant = "quest-pruned-v1"
					raw, err = candidate(f)
					if ledgerMode {
						variant = "quest-ledger-shadow-v1"
					}
					if err != nil {
						return err
					}
				}
				fmt.Printf("start case=%s repeat=%d position=%d variant=%s\n", f.ID, repeat+1, position+1, variant)
				started := time.Now()
				response, callErr := client.Generate(ctx, ai.StoryRequest{Role: "quest_evaluator", PromptVersion: "v1", Input: raw})
				row := questSuiteCall{CaseID: f.ID, Variant: variant, Repeat: repeat + 1, Position: position + 1, InputBytes: len(raw), InputSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), LatencyMS: time.Since(started).Milliseconds(), InputTokens: response.InputTokens}
				if callErr != nil {
					row.Error = callErr.Error()
				} else if !json.Valid(response.Output) {
					row.Error = "invalid JSON"
				} else {
					row.Output = response.Output
					if e := checkQuestFixture(f, response.Output); e != nil {
						row.ValidationError = e.Error()
					}
				}
				if fallback != nil && (row.Error != "" || row.ValidationError != "") {
					fallbackStarted := time.Now()
					fallbackResponse, fallbackErr := fallback.Generate(ctx, ai.StoryRequest{Role: "quest_evaluator", PromptVersion: "v1", Input: raw, Repair: true})
					row.FallbackModel = fallbackModel
					row.FallbackLatency = time.Since(fallbackStarted).Milliseconds()
					if fallbackErr != nil {
						row.FallbackError = fallbackErr.Error()
					} else if !json.Valid(fallbackResponse.Output) {
						row.FallbackError = "invalid JSON"
					} else {
						row.FallbackOutput = fallbackResponse.Output
						if e := checkQuestFixture(f, fallbackResponse.Output); e != nil {
							row.FallbackInvalid = e.Error()
						} else {
							row.Recovered = true
						}
					}
				}
				report.Calls = append(report.Calls, row)
				if err = save(); err != nil {
					return err
				}
				fmt.Printf("done latency_ms=%d error=%q validation=%q\n", row.LatencyMS, row.Error, row.ValidationError)
				if row.Error != "" && !row.Recovered && !continueOnError {
					return fmt.Errorf("provider failure; partial report saved")
				}
			}
		}
	}
	for _, call := range report.Calls {
		if (call.Error != "" || call.ValidationError != "") && !call.Recovered {
			return fmt.Errorf("semantic gate failed; report saved")
		}
	}
	return nil
}
