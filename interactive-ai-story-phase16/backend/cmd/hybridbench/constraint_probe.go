package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	pg "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	secretfile "github.com/local/interactive-ai-story/backend/internal/adapters/secrets/file"
	storyllm "github.com/local/interactive-ai-story/backend/internal/adapters/storyllm/openai_compatible"
	appaiconfig "github.com/local/interactive-ai-story/backend/internal/application/aiconfig"
	appgen "github.com/local/interactive-ai-story/backend/internal/application/generation"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	promptcatalog "github.com/local/interactive-ai-story/backend/prompts"
)

type constraintTap struct {
	ai.StoryLLM
	Input  json.RawMessage
	Output json.RawMessage
}

func (t *constraintTap) Generate(ctx context.Context, r ai.StoryRequest) (ai.StoryResponse, error) {
	t.Input = r.Input
	v, e := t.StoryLLM.Generate(ctx, r)
	t.Output = v.Output
	return v, e
}

func runConstraintProbe(dsn, secretPath, endpoint, fastModel, writerModel, output string, planContract bool) error {
	if output == "" {
		return fmt.Errorf("output required")
	}
	cases, err := loadCaseSet("cmd/hybridbench/fixtures/world-acceptance-v2.json")
	if err != nil {
		return err
	}
	var fixture benchmarkCase
	for _, f := range cases.Cases {
		if f.ID == "world-observation-negative-02" {
			fixture = f
		}
	}
	if fixture.Target == nil {
		return fmt.Errorf("fixture absent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
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
	original := prompts.Prompts["turn_planner"]
	plannerSource := "active-revision"
	if strings.TrimSpace(original) == "" {
		original, err = promptcatalog.Get("v1", "turn_planner")
		if err != nil {
			return err
		}
		plannerSource = "embedded-fallback"
	}
	candidate := original + "\nAuthoritative constraint preservation:\n- Explicit scene/context facts and prohibitions take precedence over generic novelty or dramatic escalation. The immediateGoal must not contradict scene.goal or context.previousBeatText.\n- If the context says the location is unchanged and there are no new significant details, do not turn observation into a search for changes, clues, threats, hidden activity, damage or discoveries.\n- For a bounded observation action, preserve its scope: observe existing details without inventing a new event, interaction, entry or durable change. Progress may be perceptual rather than a change in the world.\n- A plan or metaphor does not establish a new fact. Check immediateGoal against every explicit constraint before emitting the plan.\n"
	report := map[string]any{"mode": "synthetic-constraint-probe", "promptRevision": prompts.Revision, "plannerModel": fastModel, "writerModel": writerModel, "fixture": fixture, "plannerBaselinePrompt": original, "plannerCandidatePrompt": candidate, "writerPrompt": prompts.Prompts["writer"]}
	report["plannerBaselineSource"] = plannerSource
	if planContract {
		candidate += "\nOutput contract (mandatory):\nReturn only the planning JSON object, never a rewritten context or story text. Required fields: intent (string), immediateGoal (string), transition (continue_scene|new_scene|new_chapter), riskFlags (array of strings). Optional scene is an object with goal, mood, locationId; optional chapter is an object with title, goal, tone. Do not emit text, previousBeatText, recentBeats, chapterBeatCount, nextBeatPosition, worldUpdates or characterUpdates. Do not increment counters or write narrative.\nExample shape: {\"intent\":\"preserved player action\",\"immediateGoal\":\"constraint-preserving immediate goal\",\"transition\":\"continue_scene\",\"scene\":{\"goal\":\"current goal\",\"locationId\":\"exact existing location UUID\"},\"chapter\":{},\"riskFlags\":[]}\n"
		report["plannerCandidatePrompt"] = candidate
		report["mode"] = "synthetic-planner-contract-only"
	}
	rows := []map[string]any{}
	file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	save := func() error {
		report["calls"] = rows
		b, e := json.MarshalIndent(report, "", "  ")
		if e != nil {
			return e
		}
		if e = file.Truncate(0); e != nil {
			return e
		}
		if _, e = file.Seek(0, 0); e != nil {
			return e
		}
		_, e = file.Write(b)
		return e
	}
	if err := save(); err != nil {
		return err
	}
	for repeat := 0; repeat < 2; repeat++ {
		for position := 0; position < 2; position++ {
			variant := "baseline"
			prompts.Prompts["turn_planner"] = original
			if (repeat+position)%2 == 1 {
				variant = "candidate"
				prompts.Prompts["turn_planner"] = candidate
			}
			client, e := storyllm.New(storyllm.Config{BaseURL: endpoint, APIKeys: keys, Provider: "google_gemini", Model: fastModel, Profile: "constraint-planner", Timeout: 120 * time.Second, PromptSet: &prompts}, nil)
			if e != nil {
				return e
			}
			tap := &constraintTap{StoryLLM: client}
			started := time.Now()
			plan, callErr := (appgen.LLMTurnPlanner{LLM: tap}).Plan(ctx, appgen.PlayerAction{Text: fixture.Action}, *fixture.Target, nil)
			row := map[string]any{"role": "turn_planner", "variant": variant, "repeat": repeat + 1, "elapsedMs": time.Since(started).Milliseconds(), "input": tap.Input, "output": string(tap.Output), "normalizedPlan": plan}
			if callErr != nil {
				row["error"] = callErr.Error()
			}
			rows = append(rows, row)
			if err := save(); err != nil {
				return err
			}
			fmt.Printf("planner variant=%s repeat=%d elapsed=%v error=%v\n", variant, repeat+1, row["elapsedMs"], row["error"])
		}
	}
	// Writer experiment changes only the goal, not the Writer prompt or intent.
	if planContract {
		for _, row := range rows {
			if row["error"] != nil {
				return fmt.Errorf("some plans failed; report saved")
			}
		}
		return nil
	}
	const oldGoal = "Заметить новые мелкие детали или изменения вокруг экстерьера Архива во время наблюдения"
	const safeGoal = "Показать спокойное наблюдение Алекса за известным неизменным Архивом снаружи. Здание, дверь и обстановка не изменились; новых значимых деталей, повреждений, угроз, встреч или скрытой активности нет. Не входить внутрь и не вступать в разговоры."
	report["writerGoals"] = map[string]string{"recorded-drifting-goal": oldGoal, "constraint-preserving-goal": safeGoal}
	targetJSON, _ := json.Marshal(fixture.Target)
	var target map[string]json.RawMessage
	json.Unmarshal(targetJSON, &target)
	delete(target, "objectives")
	delete(target, "heroJournal")
	writer, e := storyllm.New(storyllm.Config{BaseURL: endpoint, APIKeys: keys, Provider: "google_gemini", Model: writerModel, Profile: "constraint-writer", Timeout: 5 * time.Minute, PromptSet: &prompts}, nil)
	if e != nil {
		return e
	}
	for repeat := 0; repeat < 2; repeat++ {
		for position := 0; position < 2; position++ {
			variant, goal := "recorded-drifting-goal", oldGoal
			if (repeat+position)%2 == 1 {
				variant, goal = "constraint-preserving-goal", safeGoal
			}
			input, _ := json.Marshal(map[string]any{"intent": fixture.Action, "goal": goal, "pacing": map[string]string{"transition": "continue_scene", "sceneGoal": fixture.Target.SceneGoal, "locationId": "00000000-0000-4000-8000-000000000302"}, "directorInstructions": nil, "quests": []any{}, "heroJournal": fixture.Target.Journal, "context": target})
			fmt.Printf("writer start variant=%s repeat=%d\n", variant, repeat+1)
			started := time.Now()
			response, callErr := writer.Generate(ctx, ai.StoryRequest{Role: "writer", PromptVersion: "v1", Input: input})
			row := map[string]any{"role": "writer", "variant": variant, "repeat": repeat + 1, "elapsedMs": time.Since(started).Milliseconds(), "input": json.RawMessage(input), "output": string(response.Output)}
			if callErr != nil {
				row["error"] = callErr.Error()
			}
			rows = append(rows, row)
			if err := save(); err != nil {
				return err
			}
			fmt.Printf("writer done variant=%s repeat=%d elapsed=%v error=%v\n", variant, repeat+1, row["elapsedMs"], row["error"])
		}
	}
	for _, row := range rows {
		if row["error"] != nil {
			return fmt.Errorf("some calls failed; report saved")
		}
	}
	return nil
}
