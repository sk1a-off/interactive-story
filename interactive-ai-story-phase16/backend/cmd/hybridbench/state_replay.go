package main

import (
	"context"
	"crypto/sha256"
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
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

func correctedStatePrompt(original string) (string, error) {
	const old = `"evidence":"concrete evidence"`
	if strings.Count(original, old) != 1 {
		return "", fmt.Errorf("expected exactly one legacy state schema field")
	}
	return strings.Replace(original, old, `"evidenceQuote":"short verbatim quote copied only from newBeat"`, 1), nil
}

func checkStateReplay(caseID, beat string, journal []generationtarget.JournalEntry, raw json.RawMessage) error {
	normalized, err := appgen.CheckJournalProposal(beat, journal, raw)
	if err != nil {
		return err
	}
	var changes []struct {
		EntryID  string
		Quantity *int
		Status   string
	}
	if err := json.Unmarshal(normalized, &changes); err != nil {
		return err
	}
	if strings.HasPrefix(caseID, "negative-") {
		if len(changes) != 0 {
			return fmt.Errorf("unexpected durable mutation")
		}
		return nil
	}
	for _, c := range changes {
		if c.EntryID != "00000000-0000-4000-8000-000000000101" || c.Quantity == nil {
			continue
		}
		if caseID == "spend-coins-01" && *c.Quantity == 7 && c.Status == "active" {
			return nil
		}
		if caseID == "consume-last-ration-01" && *c.Quantity == 0 && c.Status == "inactive" {
			return nil
		}
	}
	return fmt.Errorf("required resulting inventory state missing")
}

func runStateReplay(dsn, secretPath, endpoint, model, source, casePath, output string, shortQuotes, negatives bool) error {
	if output == "" {
		return fmt.Errorf("output required")
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	var saved benchmarkReport
	if err := json.Unmarshal(raw, &saved); err != nil {
		return err
	}
	caseRaw, err := os.ReadFile(casePath)
	if err != nil {
		return err
	}
	var cases struct {
		Cases []struct {
			ID     string                     `json:"id"`
			Target map[string]json.RawMessage `json:"target"`
		}
	}
	if err := json.Unmarshal(caseRaw, &cases); err != nil {
		return err
	}
	// This command never reads timelines, Reader data, or Director instructions.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
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
	original := prompts.Prompts["state_evaluator"]
	candidate, err := correctedStatePrompt(original)
	if err != nil {
		return err
	}
	if shortQuotes {
		candidate += "\nEvidence quoting contract:\n- evidenceQuote must be ONE contiguous substring of newBeat. Copy a short clause, ideally 3-20 words, sufficient to demonstrate the mutation.\n- Do not concatenate passages, insert ellipses, summarize, paraphrase, or alter punctuation. An exact short clause is better than a shortened long quotation.\n- Before returning JSON, verify that every evidenceQuote occurs literally in newBeat.\n"
	}
	report := map[string]any{"model": model, "promptRevision": prompts.Revision, "source": source, "sourceSha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "baselinePrompt": original, "candidatePrompt": candidate}
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
	failures := 0
	if negatives {
		saved.Runs = negativeStateRuns()
	}
	for repeat := 0; repeat < 2; repeat++ {
		for _, run := range saved.Runs {
			if !negatives && run.CaseID != "spend-coins-01" && run.CaseID != "consume-last-ration-01" {
				return fmt.Errorf("unsupported synthetic case %s", run.CaseID)
			}
			var target map[string]json.RawMessage
			for _, c := range cases.Cases {
				if c.ID == run.CaseID || (negatives && c.ID == "spend-coins-01") {
					target = c.Target
				}
			}
			if target == nil || run.Text == "" {
				return fmt.Errorf("missing frozen input")
			}
			var journal []generationtarget.JournalEntry
			if err := json.Unmarshal(target["heroJournal"], &journal); err != nil {
				return err
			}
			contextCopy := map[string]json.RawMessage{}
			for k, v := range target {
				if k != "heroJournal" {
					contextCopy[k] = v
				}
			}
			input, err := json.Marshal(map[string]any{"action": run.Action, "intent": run.Action, "newBeat": run.Text, "currentJournal": journal, "bootstrapJournal": false, "context": contextCopy})
			if err != nil {
				return err
			}
			for position := 0; position < 2; position++ {
				variant := "baseline"
				prompt := original
				if (repeat+position)%2 == 1 {
					variant = "candidate"
					prompt = candidate
				}
				prompts.Prompts["state_evaluator"] = prompt
				client, err := storyllm.New(storyllm.Config{BaseURL: endpoint, APIKeys: keys, Provider: "google_gemini", Model: model, Profile: "state-replay", Timeout: 120 * time.Second, PromptSet: &prompts}, nil)
				if err != nil {
					return err
				}
				started := time.Now()
				response, callErr := client.Generate(ctx, ai.StoryRequest{Role: "state_evaluator", PromptVersion: "v1", Input: input})
				row := map[string]any{"caseId": run.CaseID, "repeat": repeat + 1, "variant": variant, "elapsedMs": time.Since(started).Milliseconds(), "input": json.RawMessage(input), "output": string(response.Output)}
				if callErr != nil {
					row["error"] = callErr.Error()
				} else if e := checkStateReplay(run.CaseID, run.Text, journal, response.Output); e != nil {
					row["validationError"] = e.Error()
					if variant == "candidate" {
						failures++
					}
				}
				rows = append(rows, row)
				if err := save(); err != nil {
					return err
				}
				fmt.Printf("case=%s repeat=%d variant=%s elapsed=%v validation=%v error=%v\n", run.CaseID, repeat+1, variant, row["elapsedMs"], row["validationError"], row["error"])
				if callErr != nil {
					return callErr
				}
			}
		}
	}
	if len(rows) != len(saved.Runs)*4 {
		return fmt.Errorf("unexpected call count %d", len(rows))
	}
	if failures > 0 {
		return fmt.Errorf("candidate failed %d checks; report saved", failures)
	}
	return nil
}

func negativeStateRuns() []runResult {
	return []runResult{
		{CaseID: "negative-intent", Action: "Заплатить три монеты за ночлег.", Text: "Алекс подумал, что заплатит три монеты за ночлег, но пока не стал доставать кошелёк. Все десять монет остались при нём. Ни сделки, ни передачи предметов не произошло."},
		{CaseID: "negative-failed-purchase", Action: "Купить фонарь за три монеты.", Text: "Алекс предложил три монеты за фонарь, но торговец отказался продавать товар. Алекс не передал ему денег и не получил фонарь. Все десять монет остались у Алекса, фонарь остался у торговца."},
		{CaseID: "negative-observed-item", Action: "Осмотреть ключ на прилавке, не брать его.", Text: "Алекс увидел на прилавке железный ключ и внимательно рассмотрел его издали. Ключ принадлежал хозяину и остался лежать на прилавке. Алекс не прикасался к нему, не получал и не покупал его."},
	}
}
