package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
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

type structuralFixture struct {
	ID, Role, Beat                string
	Input                         json.RawMessage
	Target                        generationtarget.Target
	Balances                      map[string]int
	WorldType, WorldName, WorldID string
	Quest                         *questFixture
}

func structuralFixtures() []structuralFixture {
	const coin = "00000000-0000-4000-8000-000000000101"
	const ration = "00000000-0000-4000-8000-000000000102"
	journal := []generationtarget.JournalEntry{{ID: coin, Category: "currency", Name: "Монеты", Quantity: 10, Status: "active"}, {ID: ration, Category: "item", Name: "Походный паёк", Quantity: 2, Status: "active"}}
	var out []structuralFixture
	for _, tc := range []struct {
		id, action, beat string
		want             map[string]int
	}{
		{"state-purchase", "Купить еду за три монеты.", "Алекс заплатил за ужин ровно три монеты из десяти. После оплаты у него осталось семь монет. Ужин он сразу съел, походные пайки не трогал.", map[string]int{coin: 7}},
		{"state-reward", "Получить награду.", "Алекс получил пять монет в награду. В кошельке теперь пятнадцать монет. Других изменений в снаряжении не произошло.", map[string]int{coin: 15}},
		{"state-loss", "Проверить кошель.", "Из кошелька выпали две монеты и утонули. У Алекса осталось восемь монет. Пайки целы.", map[string]int{coin: 8}},
		{"state-exchange", "Продать один паёк за три монеты.", "Алекс передал покупателю один из двух походных пайков и получил три монеты. У Алекса остался один походный паёк и тринадцать монет.", map[string]int{coin: 13, ration: 1}},
		{"state-consumption", "Съесть один паёк.", "Алекс полностью съел один из двух походных пайков. Остался один походный паёк, денег он не тратил.", map[string]int{ration: 1}},
	} {
		target := generationtarget.Target{Journal: journal}
		input, _ := json.Marshal(map[string]any{"action": tc.action, "intent": tc.action, "newBeat": tc.beat, "currentJournal": journal, "bootstrapJournal": false, "context": map[string]any{"player": map[string]string{"name": "Алекс"}}})
		out = append(out, structuralFixture{ID: tc.id, Role: "state_evaluator", Beat: tc.beat, Input: input, Target: target, Balances: tc.want})
	}
	for _, r := range negativeStateRuns() {
		input, _ := json.Marshal(map[string]any{"action": r.Action, "intent": r.Action, "newBeat": r.Text, "currentJournal": journal, "bootstrapJournal": false, "context": map[string]any{"player": map[string]string{"name": "Алекс"}}})
		out = append(out, structuralFixture{ID: r.CaseID, Role: "state_evaluator", Beat: r.Text, Input: input, Target: generationtarget.Target{Journal: journal}})
	}
	const npc = "00000000-0000-4000-8000-000000000301"
	const place = "00000000-0000-4000-8000-000000000302"
	cast := json.RawMessage(`{"characters":[{"characterId":"` + npc + `","name":"Мира","age":30,"role":"Хранительница архива","personality":"Спокойная","relationship":"Нейтральное","visualAnchorEn":"Woman with dark braided hair","mood":"Спокойна","currentGoal":"Охранять архив"}]}`)
	world := json.RawMessage(`{"locations":[{"locationId":"` + place + `","name":"Архив","description":"Каменный архив с дубовой дверью","visualAnchorEn":"Stone archive with an oak door"}]}`)
	for _, tc := range []struct{ id, action, beat, kind, name, entityID string }{
		{"world-new-character", "Познакомиться с кузнецом.", "Кузнец Борис, сорокалетний мужчина с рыжей бородой, представился Алексу. Борис владеет кузницей и будет чинить снаряжение героя. Он предложил заходить за ремонтом в любое время.", "upsert_character", "Борис", ""},
		{"world-new-location", "Войти в маяк.", "Алекс вошёл в Северный маяк. Это высокая белая каменная башня с красной металлической крышей; внутри устроено постоянное убежище, куда Алекс намерен возвращаться.", "upsert_location", "Северный маяк", ""},
		{"world-existing-update", "Спросить Миру о пропаже.", "Узнав о пропаже, Мира открыто рассердилась. Теперь её цель — найти пропавшую книгу. Она осталась хранительницей архива, её внешность и отношение к Алексу не изменились.", "upsert_character", "Мира", npc},
		{"world-no-duplicate", "Посмотреть на знакомый архив.", "Алекс снова посмотрел на Архив: то же каменное здание с дубовой дверью, без каких-либо изменений. Мира по-прежнему спокойно охраняла архив.", "", "", ""},
		{"world-rumor-not-fact", "Проверить слух об уничтожении архива.", "Алекс вспомнил слух, будто Архив сгорел. Но перед ним стоял целый каменный Архив с прежней дубовой дверью. Мира подтвердила: пожара не было, слух оказался ложным.", "", "", ""},
		{"world-incidental", "Осмотреть улицу.", "Мимо прошла безымянная толпа. Алекс не познакомился ни с кем и продолжил путь. Ни одного нового значимого места он не обнаружил.", "", "", ""},
	} {
		input, _ := json.Marshal(map[string]any{"action": tc.action, "intent": tc.action, "newBeat": tc.beat, "pacing": map[string]string{"transition": "continue_scene"}, "currentCharacters": cast, "currentWorld": world, "context": map[string]any{"player": map[string]string{"name": "Алекс"}}})
		out = append(out, structuralFixture{ID: tc.id, Role: "world_evaluator", Beat: tc.beat, Input: input, Target: generationtarget.Target{InitialCast: cast, World: world}, WorldType: tc.kind, WorldName: tc.name, WorldID: tc.entityID})
	}
	quests := append(richQuestFixtures(), historyIdentityFixtures()...)
	quests = append(quests, terminalQuestFixtures()...)
	for _, q := range quests {
		q := q
		out = append(out, structuralFixture{ID: q.ID, Role: "quest_evaluator", Input: q.Input, Quest: &q})
	}
	return out
}

func checkStructural(f structuralFixture, raw json.RawMessage) error {
	if f.Quest != nil {
		return checkQuestFixture(*f.Quest, raw)
	}
	var normalized json.RawMessage
	var err error
	if f.Role == "state_evaluator" {
		normalized, err = appgen.CheckJournalProposal(f.Beat, f.Target.Journal, raw)
	} else {
		normalized, err = appgen.CheckWorldProposal(f.Beat, f.Target, raw)
	}
	if err != nil {
		return err
	}
	var changes []struct {
		Description                                          string
		Mood, CurrentGoal, VisualAnchorEn                    string
		EntryID, Status, Type, Name, CharacterID, LocationID string
		Quantity                                             *int
		Age                                                  int
	}
	if err := json.Unmarshal(normalized, &changes); err != nil {
		return err
	}
	if f.Role == "state_evaluator" {
		if len(changes) != len(f.Balances) {
			return fmt.Errorf("unexpected mutation count: %d want %d", len(changes), len(f.Balances))
		}
		seen := map[string]bool{}
		for _, c := range changes {
			want, ok := f.Balances[c.EntryID]
			if !ok || seen[c.EntryID] || c.Quantity == nil || *c.Quantity != want || c.Status != "active" {
				return fmt.Errorf("incorrect resulting inventory")
			}
			seen[c.EntryID] = true
		}
		return nil
	}
	if f.WorldType == "" {
		if len(changes) != 0 {
			return fmt.Errorf("unexpected world mutation")
		}
		return nil
	}
	// The Boris scene explicitly establishes a recurring forge. Allow it once,
	// but still require Boris and reject unrelated or duplicated entities.
	if f.ID == "world-new-character" {
		filtered := changes[:0]
		forgeCount := 0
		for _, c := range changes {
			if c.Type == "upsert_location" && strings.EqualFold(c.Name, "Кузница") {
				forgeCount++
				continue
			}
			filtered = append(filtered, c)
		}
		if forgeCount > 1 {
			return fmt.Errorf("duplicate optional forge")
		}
		changes = filtered
	}
	if len(changes) != 1 || changes[0].Type != f.WorldType || !strings.EqualFold(changes[0].Name, f.WorldName) {
		return fmt.Errorf("missing or extra world entity")
	}
	c := changes[0]
	if f.WorldID != "" && c.CharacterID != f.WorldID && c.LocationID != f.WorldID {
		return fmt.Errorf("existing entity identity lost")
	}
	if f.WorldName == "Мира" && c.Age != 30 {
		return fmt.Errorf("stable character age changed")
	}
	if f.WorldName == "Мира" && (!strings.Contains(strings.ToLower(c.CurrentGoal), "книг") || c.Mood == "" || c.Mood == "Спокойна") {
		return fmt.Errorf("observed character changes missing")
	}
	if f.WorldName == "Борис" && c.Age != 40 {
		return fmt.Errorf("explicit age not preserved")
	}
	if f.ID == "world-real-damage-control" && !strings.Contains(strings.ToLower(c.Description), "трещин") {
		return fmt.Errorf("observed damage missing")
	}
	return nil
}

func runStructuralScreen(dsn, secretPath, endpoint, model, output string, worldCompare, frozenCompare, negativeCompare bool) error {
	if output == "" {
		return fmt.Errorf("output required")
	}
	file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
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
	client, err := storyllm.New(storyllm.Config{BaseURL: endpoint, APIKeys: keys, Provider: "google_gemini", Model: model, Profile: "structural-screen", Timeout: 120 * time.Second, PromptSet: &prompts}, nil)
	if err != nil {
		return err
	}
	hashes := map[string]string{}
	for _, role := range []string{"state_evaluator", "quest_evaluator", "world_evaluator"} {
		hashes[role] = fmt.Sprintf("%x", sha256.Sum256([]byte(prompts.Prompts[role])))
	}
	fixtures := structuralFixtures()
	originalWorld := prompts.Prompts["world_evaluator"]
	candidateWorld := ""
	if worldCompare {
		candidateWorld, err = correctedWorldPrompt(originalWorld)
		if err != nil {
			return err
		}
		filtered := fixtures[:0]
		for _, f := range fixtures {
			if f.Role == "world_evaluator" {
				filtered = append(filtered, f)
			}
		}
		fixtures = filtered
		if frozenCompare {
			originalWorld = candidateWorld
			candidateWorld = worldNewBeatQuotePrompt(originalWorld)
			var frozen structuralFixture
			if err := json.Unmarshal(frozenLighthouse, &frozen); err != nil {
				return err
			}
			fixtures = append([]structuralFixture{frozen}, fixtures...)
			if negativeCompare {
				fixtures = negativeWorldFixtures()
			}
		}
	}
	report := map[string]any{"model": model, "promptRevision": prompts.Revision, "promptHashes": hashes, "fixtures": fixtures, "mode": "synthetic-fast-model-screen-no-baseline"}
	if worldCompare {
		report["mode"] = "world-prompt-balanced"
		report["baselinePrompt"] = originalWorld
		report["candidatePrompt"] = candidateWorld
		if frozenCompare {
			report["mode"] = "world-frozen-quote-balanced"
		}
		if negativeCompare {
			report["mode"] = "world-negative-balanced"
		}
	}
	rows := []map[string]any{}
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
	for repeat := 1; repeat <= 2; repeat++ {
		for _, f := range fixtures {
			positions := 1
			if worldCompare {
				positions = 2
			}
			for position := 0; position < positions; position++ {
				variant := "baseline"
				if worldCompare {
					prompt := originalWorld
					if (repeat-1+position)%2 == 1 {
						variant = "candidate"
						prompt = candidateWorld
					}
					prompts.Prompts["world_evaluator"] = prompt
					client, err = storyllm.New(storyllm.Config{BaseURL: endpoint, APIKeys: keys, Provider: "google_gemini", Model: model, Profile: "world-prompt-compare", Timeout: 120 * time.Second, PromptSet: &prompts}, nil)
					if err != nil {
						return err
					}
				}
				fmt.Printf("start role=%s case=%s repeat=%d\n", f.Role, f.ID, repeat)
				started := time.Now()
				response, callErr := client.Generate(ctx, ai.StoryRequest{Role: f.Role, PromptVersion: "v1", Input: f.Input})
				row := map[string]any{"role": f.Role, "caseId": f.ID, "repeat": repeat, "elapsedMs": time.Since(started).Milliseconds(), "output": string(response.Output)}
				row["variant"] = variant
				if callErr != nil {
					row["error"] = callErr.Error()
					failures++
				} else if e := checkStructural(f, response.Output); e != nil {
					row["validationError"] = e.Error()
					failures++
				}
				rows = append(rows, row)
				if err := save(); err != nil {
					return err
				}
				fmt.Printf("done elapsed=%v error=%v validation=%v\n", row["elapsedMs"], row["error"], row["validationError"])
			}
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d checks failed; all results saved", failures)
	}
	return nil
}

//go:embed fixtures/world-lighthouse-replay.json
var frozenLighthouse []byte

func worldNewBeatQuotePrompt(original string) string {
	return original + "\nNew-beat quote selection:\n- evidenceQuote is ONE short contiguous substring copied literally from newBeat, preferably a complete clause of 3-20 words. Never insert ellipses, join passages, paraphrase, or change punctuation.\n- Existing context may identify the place/person and its established appearance, but is never the source of evidenceQuote. Do not quote previousBeatText, action, intent, or currentWorld.\n- If the place name or exterior appears only in older context, choose a newBeat clause proving arrival, entry, exploration or an observed durable feature. The quote does not need to repeat the place name. Do not fabricate a named description to combine identity and evidence.\n- Before returning JSON, verify every evidenceQuote occurs literally in newBeat and supports the proposed change. If no new evidence supports it, omit the change.\n"
}

func correctedWorldPrompt(original string) (string, error) {
	const character = `"currentGoal":"..."}`
	const location = `"visualAnchorEn":"English visual anchor"}`
	if strings.Count(original, character) != 1 || strings.Count(original, location) != 1 {
		return "", fmt.Errorf("unexpected world JSON example; refuse blind override")
	}
	quote := `,"evidenceQuote":"short verbatim quote copied only from newBeat"}`
	updated := strings.Replace(original, character, `"currentGoal":"..."`+quote, 1)
	updated = strings.Replace(updated, location, `"visualAnchorEn":"English visual anchor"`+quote, 1)
	return updated, nil
}
