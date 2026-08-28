package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	pgadapter "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	secretfile "github.com/local/interactive-ai-story/backend/internal/adapters/secrets/file"
	storyllm "github.com/local/interactive-ai-story/backend/internal/adapters/storyllm/openai_compatible"
	appaiconfig "github.com/local/interactive-ai-story/backend/internal/application/aiconfig"
	appgen "github.com/local/interactive-ai-story/backend/internal/application/generation"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

var defaultFastRoles = map[string]bool{
	"action_interpreter": true,
	"pacing":             true,
	"world_evaluator":    true,
	"state_evaluator":    true,
	"quest_evaluator":    true,
	"choices":            true,
}

type roleCall struct {
	Role         string `json:"role"`
	Model        string `json:"model"`
	ElapsedMS    int64  `json:"elapsedMs"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	Output       string `json:"output,omitempty"`
	Error        string `json:"error,omitempty"`
}

type roleRouter struct {
	fast, quality aiport.StoryLLM
	fastRoles     map[string]bool
	mu            sync.Mutex
	calls         []roleCall
}

func (r *roleRouter) Identity() aiport.ProviderIdentity {
	return aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "google_gemini", Model: "hybrid-flash-lite-antigravity", Profile: "benchmark-only"}
}

func (r *roleRouter) Generate(ctx context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	client := r.quality
	if r.fastRoles[request.Role] {
		client = r.fast
	}
	started := time.Now()
	response, err := client.Generate(ctx, request)
	call := roleCall{Role: request.Role, Model: client.Identity().Model, ElapsedMS: time.Since(started).Milliseconds(), InputTokens: response.InputTokens, OutputTokens: response.OutputTokens, Output: string(response.Output)}
	if err != nil {
		call.Error = err.Error()
	}
	r.mu.Lock()
	r.calls = append(r.calls, call)
	r.mu.Unlock()
	return response, err
}

type staticTarget struct{ target generationtarget.Target }

func (s staticTarget) CurrentWriteTarget(context.Context, timeline.ID) (generationtarget.Target, error) {
	return s.target, nil
}

type staticInstructions struct {
	values []narrative.DirectorInstruction
}

func (s staticInstructions) ActiveInstructions(context.Context, timeline.ID) ([]narrative.DirectorInstruction, error) {
	return append([]narrative.DirectorInstruction(nil), s.values...), nil
}

type captureCanon struct {
	head   int64
	events []event.StoredEvent
}

func (c *captureCanon) AppendSemantic(_ context.Context, timelineID timeline.ID, expected int64, pending []event.PendingEvent) ([]event.StoredEvent, error) {
	if expected != c.head {
		return nil, fmt.Errorf("capture head moved: expected %d, have %d", expected, c.head)
	}
	stored := make([]event.StoredEvent, 0, len(pending))
	for _, item := range pending {
		eventID, err := id.New()
		if err != nil {
			return nil, err
		}
		c.head++
		stored = append(stored, event.StoredEvent{ID: event.ID(eventID), TimelineID: timelineID, Seq: c.head, Type: item.Type, SchemaVersion: item.SchemaVersion, Payload: append([]byte(nil), item.Payload...), GenerationID: item.GenerationID, CreatedAt: time.Now().UTC()})
	}
	c.events = append(c.events, stored...)
	return stored, nil
}

type eventResult struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type qualityMetrics struct {
	Words                int     `json:"words"`
	Paragraphs           int     `json:"paragraphs"`
	RepeatedSixGramRatio float64 `json:"repeatedSixGramRatio"`
	DuplicateParagraphs  int     `json:"duplicateParagraphs"`
	CyrillicRatio        float64 `json:"cyrillicRatio"`
	ActionTermCoverage   float64 `json:"actionTermCoverage"`
	ChoiceCount          int     `json:"choiceCount"`
	WorldChanges         int     `json:"worldChanges"`
	JournalChanges       int     `json:"journalChanges"`
	ObjectiveChanges     int     `json:"objectiveChanges"`
}

type runResult struct {
	Action    string         `json:"action"`
	ElapsedMS int64          `json:"elapsedMs"`
	Error     string         `json:"error,omitempty"`
	Calls     []roleCall     `json:"calls"`
	Text      string         `json:"text,omitempty"`
	Choices   []string       `json:"choices,omitempty"`
	Events    []eventResult  `json:"events,omitempty"`
	Quality   qualityMetrics `json:"quality"`
}

type benchmarkReport struct {
	StartedAt       time.Time              `json:"startedAt"`
	FinishedAt      time.Time              `json:"finishedAt"`
	TimelineID      string                 `json:"timelineId"`
	FastModel       string                 `json:"fastModel"`
	QualityModel    string                 `json:"qualityModel"`
	FastRoles       []string               `json:"fastRoles"`
	PromptRevision  int64                  `json:"promptRevision"`
	Runs            []runResult            `json:"runs"`
	Summary         map[string]interface{} `json:"summary"`
	BaselineRuns    []runResult            `json:"baselineRuns,omitempty"`
	BaselineSummary map[string]interface{} `json:"baselineSummary,omitempty"`
}

func main() {
	var databaseURL, secretPath, timelineText, endpoint, fastModel, qualityModel, output, actionList, fastRoleList string
	var runs int
	var compare bool
	flag.StringVar(&databaseURL, "database-url", "postgres://story:story@127.0.0.1:5432/story?sslmode=disable", "PostgreSQL URL used read-only except for normal connection state")
	flag.StringVar(&secretPath, "secret-store", "../data/secrets/provider-keys.json", "provider key store")
	flag.StringVar(&timelineText, "timeline", "", "timeline UUID to snapshot")
	flag.StringVar(&endpoint, "endpoint", "https://generativelanguage.googleapis.com/v1beta/openai", "Google AI OpenAI-compatible base URL")
	flag.StringVar(&fastModel, "fast-model", "gemini-3.5-flash-lite", "model for structural roles")
	flag.StringVar(&qualityModel, "quality-model", "antigravity-preview-05-2026", "model for director and writer")
	flag.StringVar(&output, "output", "", "JSON report path")
	flag.StringVar(&actionList, "actions", "", "optional actions separated by ||; defaults to the current Reader choices")
	flag.StringVar(&fastRoleList, "fast-roles", "", "optional comma-separated role override")
	flag.IntVar(&runs, "runs", 3, "number of independent actions from the current choice set")
	flag.BoolVar(&compare, "compare", false, "also run every action with the quality model for every role on the exact same snapshot")
	flag.Parse()
	if timelineText == "" || output == "" || runs < 1 {
		fmt.Fprintln(os.Stderr, "-timeline, -output and -runs >= 1 are required")
		os.Exit(2)
	}
	parsedTimelineID, err := id.Parse(timelineText)
	check(err)
	timelineID := timeline.ID(parsedTimelineID)

	timeoutMultiplier := 6
	if compare {
		timeoutMultiplier = 12
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(runs*timeoutMultiplier)*time.Minute)
	defer cancel()
	pool, err := bootstrap.OpenPostgres(ctx, databaseURL)
	check(err)
	defer pool.Close()

	configRepo := pgadapter.NewAIConfigRepository(pool)
	promptRepo := pgadapter.NewPromptSetRepository(pool)
	active, err := configRepo.Active(ctx)
	check(err)
	store, err := secretfile.New(secretPath)
	check(err)
	keys := appaiconfig.CredentialKeys(ctx, store, active.ID)
	if len(keys) == 0 {
		check(errors.New("active AI revision has no Google credentials"))
	}
	promptRevision, err := promptRepo.Active(ctx)
	check(err)

	target, err := pgadapter.NewGenerationTarget(pool).CurrentWriteTarget(ctx, timelineID)
	check(err)
	directorRepo := pgadapter.NewDirectorRepository(pool)
	instructions, err := directorRepo.ActiveInstructions(ctx, timelineID)
	check(err)
	current, err := pgadapter.NewReaderRepository(pool).Current(ctx, timelineID)
	check(err)
	actions := current.Choices
	if strings.TrimSpace(actionList) != "" {
		actions = actions[:0]
		for _, action := range strings.Split(actionList, "||") {
			if action = strings.TrimSpace(action); action != "" {
				actions = append(actions, action)
			}
		}
	}
	if len(actions) == 0 {
		check(errors.New("current Reader view has no choices"))
	}

	fast, err := storyllm.New(storyllm.Config{BaseURL: endpoint, APIKeys: keys, Provider: "google_gemini", Model: fastModel, Profile: "hybrid-fast", Timeout: 120 * time.Second, PromptSet: &promptRevision}, nil)
	check(err)
	quality, err := storyllm.New(storyllm.Config{BaseURL: endpoint, APIKeys: keys, Provider: "google_gemini", Model: qualityModel, Profile: "hybrid-quality", Timeout: 5 * time.Minute, PromptSet: &promptRevision}, nil)
	check(err)

	selectedFastRoles := defaultFastRoles
	if strings.TrimSpace(fastRoleList) != "" {
		selectedFastRoles = map[string]bool{}
		for _, role := range strings.Split(fastRoleList, ",") {
			if role = strings.TrimSpace(role); role != "" {
				selectedFastRoles[role] = true
			}
		}
	}
	roles := make([]string, 0, len(selectedFastRoles))
	for role := range selectedFastRoles {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	report := benchmarkReport{StartedAt: time.Now().UTC(), TimelineID: string(timelineID), FastModel: fastModel, QualityModel: qualityModel, FastRoles: roles, PromptRevision: promptRevision.Revision}
	for index := 0; index < runs; index++ {
		action := actions[index%len(actions)]
		router := &roleRouter{fast: fast, quality: quality, fastRoles: selectedFastRoles}
		canon := &captureCanon{}
		pipeline := appgen.Pipeline{LLM: router, MaxRepairs: 1, Canon: canon, Instructions: staticInstructions{values: instructions}, Targets: staticTarget{target: target}}
		generationID, idErr := id.New()
		check(idErr)
		started := time.Now()
		_, runErr := pipeline.Run(ctx, generationID, appgen.PlayerAction{TimelineID: timelineID, ExpectedHead: 0, Text: action})
		result := buildRunResult(action, time.Since(started), router.calls, canon.events, target.PreviousBeatText, runErr)
		report.Runs = append(report.Runs, result)
		fmt.Printf("run=%d elapsed=%.2fs calls=%d words=%d paragraphs=%d choices=%d error=%s\n", index+1, float64(result.ElapsedMS)/1000, len(result.Calls), result.Quality.Words, result.Quality.Paragraphs, result.Quality.ChoiceCount, result.Error)
	}
	if compare {
		for index := 0; index < runs; index++ {
			action := actions[index%len(actions)]
			router := &roleRouter{fast: quality, quality: quality, fastRoles: selectedFastRoles}
			canon := &captureCanon{}
			pipeline := appgen.Pipeline{LLM: router, MaxRepairs: 1, Canon: canon, Instructions: staticInstructions{values: instructions}, Targets: staticTarget{target: target}}
			generationID, idErr := id.New()
			check(idErr)
			started := time.Now()
			_, runErr := pipeline.Run(ctx, generationID, appgen.PlayerAction{TimelineID: timelineID, ExpectedHead: 0, Text: action})
			result := buildRunResult(action, time.Since(started), router.calls, canon.events, target.PreviousBeatText, runErr)
			report.BaselineRuns = append(report.BaselineRuns, result)
			fmt.Printf("baseline_run=%d elapsed=%.2fs calls=%d words=%d paragraphs=%d choices=%d error=%s\n", index+1, float64(result.ElapsedMS)/1000, len(result.Calls), result.Quality.Words, result.Quality.Paragraphs, result.Quality.ChoiceCount, result.Error)
		}
		report.BaselineSummary = summarize(report.BaselineRuns)
	}
	report.FinishedAt = time.Now().UTC()
	report.Summary = summarize(report.Runs)
	raw, err := json.MarshalIndent(report, "", "  ")
	check(err)
	check(os.MkdirAll(filepath.Dir(output), 0o755))
	check(os.WriteFile(output, append(raw, '\n'), 0o644))
	fmt.Printf("summary=%s\n", mustCompactJSON(report.Summary))
}

func buildRunResult(action string, elapsed time.Duration, calls []roleCall, events []event.StoredEvent, previous string, runErr error) runResult {
	result := runResult{Action: action, ElapsedMS: elapsed.Milliseconds(), Calls: append([]roleCall(nil), calls...)}
	if runErr != nil {
		result.Error = runErr.Error()
	}
	for _, stored := range events {
		result.Events = append(result.Events, eventResult{Type: stored.Type, Payload: append([]byte(nil), stored.Payload...)})
		switch stored.Type {
		case "beat_committed":
			var payload struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(stored.Payload, &payload)
			result.Text = payload.Text
		case "choices_ready":
			var payload struct {
				Choices []string `json:"choices"`
			}
			_ = json.Unmarshal(stored.Payload, &payload)
			result.Choices = payload.Choices
		case "character_world_upserted", "location_world_upserted":
			result.Quality.WorldChanges++
		case "journal_entry_created", "journal_entry_updated":
			result.Quality.JournalChanges++
		case "objective_created", "objective_updated":
			result.Quality.ObjectiveChanges++
		}
	}
	result.Quality = measureQuality(action, previous, result.Text, result.Choices, result.Quality)
	return result
}

func measureQuality(action, previous, text string, choices []string, base qualityMetrics) qualityMetrics {
	paragraphs := splitParagraphs(text)
	base.Words = len(strings.Fields(text))
	base.Paragraphs = len(paragraphs)
	base.RepeatedSixGramRatio = repeatedNGramRatio(text, 6)
	base.DuplicateParagraphs = duplicateParagraphs(paragraphs)
	base.CyrillicRatio = cyrillicRatio(text)
	base.ActionTermCoverage = termCoverage(action, text)
	base.ChoiceCount = len(choices)
	_ = previous // Production novelty validation already rejects copied prior prose.
	return base
}

func summarize(runs []runResult) map[string]interface{} {
	elapsed := make([]int64, 0, len(runs))
	valid, totalWords, totalParagraphs := 0, 0, 0
	roleElapsed := map[string][]int64{}
	for _, run := range runs {
		elapsed = append(elapsed, run.ElapsedMS)
		if run.Error == "" && run.Quality.ChoiceCount == 4 && run.Text != "" {
			valid++
		}
		totalWords += run.Quality.Words
		totalParagraphs += run.Quality.Paragraphs
		for _, call := range run.Calls {
			roleElapsed[call.Role] = append(roleElapsed[call.Role], call.ElapsedMS)
		}
	}
	roles := map[string]interface{}{}
	for role, values := range roleElapsed {
		roles[role] = map[string]interface{}{"medianMs": median(values), "calls": len(values)}
	}
	count := max(1, len(runs))
	return map[string]interface{}{
		"runs": len(runs), "validRuns": valid, "medianElapsedMs": median(elapsed),
		"averageWords": float64(totalWords) / float64(count), "averageParagraphs": float64(totalParagraphs) / float64(count),
		"roleLatency": roles,
	}
}

func median(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	copyValues := append([]int64(nil), values...)
	sort.Slice(copyValues, func(i, j int) bool { return copyValues[i] < copyValues[j] })
	middle := len(copyValues) / 2
	if len(copyValues)%2 == 1 {
		return copyValues[middle]
	}
	return (copyValues[middle-1] + copyValues[middle]) / 2
}

func splitParagraphs(text string) []string {
	text = strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n")
	parts := strings.Split(text, "\n\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func normalizeWords(text string) []string {
	var builder strings.Builder
	for _, char := range strings.ToLower(text) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			builder.WriteRune(char)
		} else {
			builder.WriteByte(' ')
		}
	}
	return strings.Fields(builder.String())
}

func repeatedNGramRatio(text string, size int) float64 {
	words := normalizeWords(text)
	if len(words) < size {
		return 0
	}
	counts := map[string]int{}
	for index := 0; index+size <= len(words); index++ {
		counts[strings.Join(words[index:index+size], " ")]++
	}
	repeated := 0
	for _, count := range counts {
		if count > 1 {
			repeated += count - 1
		}
	}
	return float64(repeated) / float64(len(words)-size+1)
}

func duplicateParagraphs(paragraphs []string) int {
	seen, duplicates := map[string]bool{}, 0
	for _, paragraph := range paragraphs {
		key := strings.Join(normalizeWords(paragraph), " ")
		if key != "" && seen[key] {
			duplicates++
		}
		seen[key] = true
	}
	return duplicates
}

func cyrillicRatio(text string) float64 {
	cyrillic, latin := 0, 0
	for _, char := range text {
		if unicode.In(char, unicode.Cyrillic) {
			cyrillic++
		} else if unicode.In(char, unicode.Latin) {
			latin++
		}
	}
	if cyrillic+latin == 0 {
		return 0
	}
	return float64(cyrillic) / float64(cyrillic+latin)
}

func termCoverage(action, text string) float64 {
	terms, body := normalizeWords(action), map[string]bool{}
	for _, word := range normalizeWords(text) {
		body[word] = true
	}
	seen, matched, eligible := map[string]bool{}, 0, 0
	for _, term := range terms {
		if len([]rune(term)) < 4 || seen[term] {
			continue
		}
		seen[term], eligible = true, eligible+1
		if body[term] {
			matched++
		}
	}
	if eligible == 0 {
		return 0
	}
	return float64(matched) / float64(eligible)
}

func mustCompactJSON(value interface{}) string { raw, _ := json.Marshal(value); return string(raw) }

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
