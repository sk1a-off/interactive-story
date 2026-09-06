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
	"github.com/local/interactive-ai-story/backend/internal/ports/generationmetrics"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

type roleCall struct {
	Role                 string         `json:"role"`
	Model                string         `json:"model"`
	ElapsedMS            int64          `json:"elapsedMs"`
	InputTokens          int64          `json:"inputTokens"`
	OutputTokens         int64          `json:"outputTokens"`
	InputBytes           int            `json:"inputBytes"`
	InputEstimatedTokens int            `json:"inputEstimatedTokens"`
	ExactDuplicateBytes  int            `json:"exactDuplicateBytes"`
	InputBreakdown       map[string]int `json:"inputBreakdown,omitempty"`
	Output               string         `json:"output,omitempty"`
	Error                string         `json:"error,omitempty"`
	EscalatedFrom        string         `json:"escalatedFrom,omitempty"`
	Repair               bool           `json:"repair,omitempty"`
}

type roleRouter struct {
	fast, quality aiport.StoryLLM
	fastRoles     map[string]bool
	mu            sync.Mutex
	calls         []roleCall
	saveSizes     bool
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
	inputBytes, estimatedTokens, exactDuplicateBytes, inputBreakdown := measureRoleInput(request.Input)
	call := roleCall{
		Role: request.Role, Model: client.Identity().Model, ElapsedMS: time.Since(started).Milliseconds(),
		InputTokens: response.InputTokens, OutputTokens: response.OutputTokens,
		InputBytes: inputBytes, InputEstimatedTokens: estimatedTokens,
		ExactDuplicateBytes: exactDuplicateBytes, InputBreakdown: inputBreakdown,
		Output: string(response.Output), EscalatedFrom: response.EscalatedFrom, Repair: request.Repair,
	}
	if !r.saveSizes {
		call.InputBreakdown = nil
	}
	if err != nil {
		call.Error = err.Error()
	}
	r.mu.Lock()
	r.calls = append(r.calls, call)
	r.mu.Unlock()
	return response, err
}

// measureRoleInput records sizes only. It deliberately does not persist the
// role input, so benchmark reports cannot leak story or credential material.
// ExactDuplicateBytes counts top-level values that are byte-for-byte repeated
// as a field inside the full generation context.
func measureRoleInput(raw []byte) (int, int, int, map[string]int) {
	breakdown := map[string]int{}
	var input map[string]json.RawMessage
	if json.Unmarshal(raw, &input) != nil {
		return len(raw), estimateTokens(raw), 0, breakdown
	}
	for key, value := range input {
		breakdown[key] = len(value)
	}
	var contextFields map[string]json.RawMessage
	if contextRaw, ok := input["context"]; ok {
		_ = json.Unmarshal(contextRaw, &contextFields)
	}
	duplicates := 0
	for key, value := range input {
		if key == "context" {
			continue
		}
		for _, contextValue := range contextFields {
			if string(value) == string(contextValue) {
				duplicates += len(value)
				break
			}
		}
	}
	return len(raw), estimateTokens(raw), duplicates, breakdown
}

func estimateTokens(raw []byte) int {
	if len(raw) == 0 {
		return 0
	}
	return (len([]rune(string(raw))) + 3) / 4
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
	CaseID                 string                    `json:"caseId,omitempty"`
	Category               string                    `json:"category,omitempty"`
	Action                 string                    `json:"action"`
	ElapsedMS              int64                     `json:"elapsedMs"`
	Error                  string                    `json:"error,omitempty"`
	Calls                  []roleCall                `json:"calls"`
	Text                   string                    `json:"text,omitempty"`
	Choices                []string                  `json:"choices,omitempty"`
	Events                 []eventResult             `json:"events,omitempty"`
	Quality                qualityMetrics            `json:"quality"`
	ExpectedImportantFacts []string                  `json:"expectedImportantFacts,omitempty"`
	ValidatorResults       map[string]bool           `json:"validatorResults,omitempty"`
	StageMetrics           generationmetrics.Metrics `json:"stageMetrics"`
}

type benchmarkReport struct {
	StartedAt       time.Time              `json:"startedAt"`
	FinishedAt      time.Time              `json:"finishedAt"`
	TimelineID      string                 `json:"timelineId"`
	FastModel       string                 `json:"fastModel"`
	QualityModel    string                 `json:"qualityModel"`
	FastRoles       []string               `json:"fastRoles"`
	PromptRevision  int64                  `json:"promptRevision"`
	Profile         string                 `json:"profile"`
	CaseSet         string                 `json:"caseSet,omitempty"`
	CaseSetRevision int                    `json:"caseSetRevision,omitempty"`
	PlannerMode     string                 `json:"plannerMode"`
	ExtractorMode   string                 `json:"extractorMode"`
	ContextMode     string                 `json:"contextMode"`
	Runs            []runResult            `json:"runs"`
	Summary         map[string]interface{} `json:"summary"`
	BaselineRuns    []runResult            `json:"baselineRuns,omitempty"`
	BaselineSummary map[string]interface{} `json:"baselineSummary,omitempty"`
	PairedDelta     map[string]interface{} `json:"pairedDelta,omitempty"`
}

func main() {
	var databaseURL, secretPath, timelineText, endpoint, fastModel, qualityModel, output, actionList, fastRoleList string
	var profile, caseSetPath, plannerMode, extractorMode, contextMode string
	var caseFilter string
	var runs int
	var compare bool
	var saveRoleInputSizes bool
	flag.StringVar(&databaseURL, "database-url", "postgres://story:story@127.0.0.1:5432/story?sslmode=disable", "PostgreSQL URL used read-only except for normal connection state")
	flag.StringVar(&secretPath, "secret-store", "../data/secrets/provider-keys.json", "provider key store")
	flag.StringVar(&timelineText, "timeline", "", "timeline UUID to snapshot")
	flag.StringVar(&endpoint, "endpoint", "https://generativelanguage.googleapis.com/v1beta/openai", "Google AI OpenAI-compatible base URL")
	flag.StringVar(&fastModel, "fast-model", "gemini-3.5-flash-lite", "model for structural roles")
	flag.StringVar(&qualityModel, "quality-model", "antigravity-preview-05-2026", "model for director and writer")
	flag.StringVar(&output, "output", "", "JSON report path")
	flag.StringVar(&actionList, "actions", "", "optional actions separated by ||; defaults to the current Reader choices")
	flag.StringVar(&fastRoleList, "fast-roles", "", "optional comma-separated role override")
	flag.StringVar(&profile, "profile", "baseline-safe", "baseline-safe, aggressive-hybrid, or antigravity-only")
	flag.StringVar(&caseSetPath, "case-set", "", "optional revisioned benchmark case-set JSON")
	flag.StringVar(&caseFilter, "case-filter", "", "optional comma-separated case IDs or categories")
	flag.StringVar(&plannerMode, "planner-mode", "legacy", "legacy, flash-lite, or antigravity")
	flag.StringVar(&extractorMode, "extractor-mode", "legacy", "legacy, flash-lite, flash-lite-repair, or antigravity")
	flag.StringVar(&contextMode, "context-mode", "lossless-dedupe-v1", "context implementation label recorded in the report")
	flag.IntVar(&runs, "runs", 3, "number of independent actions from the current choice set")
	flag.BoolVar(&compare, "compare", false, "also run every action with the quality model for every role on the exact same snapshot")
	flag.BoolVar(&saveRoleInputSizes, "save-role-input-sizes", true, "persist aggregate per-role input sizes (never input content)")
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
	if strings.TrimSpace(caseSetPath) != "" {
		timeoutMultiplier *= 12
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
	var cases []benchmarkCase
	var loadedCaseSet benchmarkCaseSet
	if strings.TrimSpace(caseSetPath) != "" {
		loadedCaseSet, err = loadCaseSet(caseSetPath)
		check(err)
		cases, err = filterCases(loadedCaseSet.Cases, caseFilter)
		check(err)
		actions = make([]string, 0, len(cases))
		for _, item := range cases {
			actions = append(actions, item.Action)
		}
	}
	if strings.TrimSpace(actionList) != "" {
		cases = nil
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

	selectedFastRoles, err := fastRolesForProfile(profile)
	check(err)
	if strings.TrimSpace(fastRoleList) != "" {
		selectedFastRoles = map[string]bool{}
		for _, role := range strings.Split(fastRoleList, ",") {
			if role = strings.TrimSpace(role); role != "" {
				selectedFastRoles[role] = true
			}
		}
	}
	switch plannerMode {
	case "legacy":
	case "flash-lite":
		selectedFastRoles["turn_planner"] = true
	case "antigravity":
		delete(selectedFastRoles, "turn_planner")
	default:
		check(fmt.Errorf("unknown planner mode %q", plannerMode))
	}
	switch extractorMode {
	case "legacy":
	case "flash-lite", "flash-lite-repair":
		selectedFastRoles["canon_extractor"] = true
	case "antigravity":
		delete(selectedFastRoles, "canon_extractor")
	default:
		check(fmt.Errorf("unknown extractor mode %q", extractorMode))
	}
	roles := make([]string, 0, len(selectedFastRoles))
	for role := range selectedFastRoles {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	report := benchmarkReport{StartedAt: time.Now().UTC(), TimelineID: string(timelineID), FastModel: fastModel, QualityModel: qualityModel, FastRoles: roles, PromptRevision: promptRevision.Revision, Profile: profile, CaseSet: loadedCaseSet.Name, CaseSetRevision: loadedCaseSet.Revision, PlannerMode: plannerMode, ExtractorMode: extractorMode, ContextMode: contextMode}
	totalRuns := runs
	caseRuns := expandCaseRuns(cases, runs)
	if len(cases) > 0 {
		totalRuns = len(caseRuns)
	}
	caseAt := func(index int) *benchmarkCase {
		if len(caseRuns) == 0 {
			return nil
		}
		return &caseRuns[index]
	}
	actionAt := func(index int) string {
		if item := caseAt(index); item != nil {
			return item.Action
		}
		return actions[index%len(actions)]
	}
	for index := 0; index < totalRuns; index++ {
		action := actionAt(index)
		router := &roleRouter{fast: fast, quality: quality, fastRoles: selectedFastRoles, saveSizes: saveRoleInputSizes}
		canon := &captureCanon{}
		var stageMetrics generationmetrics.Metrics
		pipeline := appgen.Pipeline{LLM: router, MaxRepairs: 1, Canon: canon, Instructions: staticInstructions{values: instructions}, Targets: staticTarget{target: target}, Metrics: func(metrics generationmetrics.Metrics) { stageMetrics = metrics }}
		if plannerMode != "legacy" {
			pipeline.Planner = appgen.LLMTurnPlanner{LLM: router}
		}
		if extractorMode != "legacy" {
			maxExtractorRepairs := 0
			if extractorMode == "flash-lite-repair" {
				maxExtractorRepairs = 1
			}
			pipeline.Extractor = appgen.LLMCanonExtractor{LLM: router, MaxRepairs: maxExtractorRepairs}
		}
		generationID, idErr := id.New()
		check(idErr)
		started := time.Now()
		_, runErr := pipeline.Run(ctx, generationID, appgen.PlayerAction{TimelineID: timelineID, ExpectedHead: 0, Text: action})
		result := buildRunResult(action, time.Since(started), router.calls, canon.events, target.PreviousBeatText, runErr)
		result.StageMetrics = stageMetrics
		if item := caseAt(index); item != nil {
			result.CaseID, result.Category = item.ID, item.Category
			result.ExpectedImportantFacts = append([]string(nil), item.ExpectedImportantFacts...)
			result.ValidatorResults = validateGoldenDeltas(result.Events, item.GoldenDurableDeltas)
		}
		report.Runs = append(report.Runs, result)
		fmt.Printf("run=%d elapsed=%.2fs calls=%d words=%d paragraphs=%d choices=%d error=%s\n", index+1, float64(result.ElapsedMS)/1000, len(result.Calls), result.Quality.Words, result.Quality.Paragraphs, result.Quality.ChoiceCount, result.Error)
	}
	if compare {
		for index := 0; index < totalRuns; index++ {
			action := actionAt(index)
			router := &roleRouter{fast: quality, quality: quality, fastRoles: selectedFastRoles, saveSizes: saveRoleInputSizes}
			canon := &captureCanon{}
			var stageMetrics generationmetrics.Metrics
			pipeline := appgen.Pipeline{LLM: router, MaxRepairs: 1, Canon: canon, Instructions: staticInstructions{values: instructions}, Targets: staticTarget{target: target}, Metrics: func(metrics generationmetrics.Metrics) { stageMetrics = metrics }}
			if plannerMode != "legacy" {
				pipeline.Planner = appgen.LLMTurnPlanner{LLM: router}
			}
			if extractorMode != "legacy" {
				maxExtractorRepairs := 0
				if extractorMode == "flash-lite-repair" {
					maxExtractorRepairs = 1
				}
				pipeline.Extractor = appgen.LLMCanonExtractor{LLM: router, MaxRepairs: maxExtractorRepairs}
			}
			generationID, idErr := id.New()
			check(idErr)
			started := time.Now()
			_, runErr := pipeline.Run(ctx, generationID, appgen.PlayerAction{TimelineID: timelineID, ExpectedHead: 0, Text: action})
			result := buildRunResult(action, time.Since(started), router.calls, canon.events, target.PreviousBeatText, runErr)
			result.StageMetrics = stageMetrics
			if item := caseAt(index); item != nil {
				result.CaseID, result.Category = item.ID, item.Category
				result.ExpectedImportantFacts = append([]string(nil), item.ExpectedImportantFacts...)
				result.ValidatorResults = validateGoldenDeltas(result.Events, item.GoldenDurableDeltas)
			}
			report.BaselineRuns = append(report.BaselineRuns, result)
			fmt.Printf("baseline_run=%d elapsed=%.2fs calls=%d words=%d paragraphs=%d choices=%d error=%s\n", index+1, float64(result.ElapsedMS)/1000, len(result.Calls), result.Quality.Words, result.Quality.Paragraphs, result.Quality.ChoiceCount, result.Error)
		}
		report.BaselineSummary = summarize(report.BaselineRuns)
		report.PairedDelta = pairedDelta(report.Runs, report.BaselineRuns)
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
	valid, totalWords, totalParagraphs, totalCalls, repairs, escalations := 0, 0, 0, 0, 0, 0
	roleElapsed := map[string][]int64{}
	modelCalls := map[string]int{}
	for _, run := range runs {
		elapsed = append(elapsed, run.ElapsedMS)
		if run.Error == "" && run.Quality.ChoiceCount == 4 && run.Text != "" {
			valid++
		}
		totalWords += run.Quality.Words
		totalParagraphs += run.Quality.Paragraphs
		for _, call := range run.Calls {
			totalCalls++
			modelCalls[call.Model]++
			if call.Repair || call.Role == "structured_repair" {
				repairs++
			}
			if call.EscalatedFrom != "" {
				escalations++
			}
			roleElapsed[call.Role] = append(roleElapsed[call.Role], call.ElapsedMS)
		}
	}
	roles := map[string]interface{}{}
	for role, values := range roleElapsed {
		roles[role] = map[string]interface{}{"p50Ms": percentile(values, 50), "p95Ms": percentile(values, 95), "maxMs": percentile(values, 100), "calls": len(values)}
	}
	count := max(1, len(runs))
	return map[string]interface{}{
		"runs": len(runs), "validRuns": valid, "p50ElapsedMs": percentile(elapsed, 50), "p95ElapsedMs": percentile(elapsed, 95), "maxElapsedMs": percentile(elapsed, 100),
		"averageWords": float64(totalWords) / float64(count), "averageParagraphs": float64(totalParagraphs) / float64(count),
		"roleLatency": roles, "totalCalls": totalCalls, "modelCalls": modelCalls, "repairs": repairs, "escalations": escalations,
	}
}

func validateGoldenDeltas(events []eventResult, deltas []goldenDelta) map[string]bool {
	counts := map[string]int{}
	for _, item := range events {
		counts[item.Type]++
	}
	results := make(map[string]bool, len(deltas))
	for _, delta := range deltas {
		count := counts[delta.EventType]
		results[delta.EventType] = count >= delta.Minimum && (delta.Maximum < 0 || count <= delta.Maximum)
	}
	return results
}

func pairedDelta(candidate, baseline []runResult) map[string]interface{} {
	count := min(len(candidate), len(baseline))
	if count == 0 {
		return nil
	}
	var total int64
	faster := 0
	for index := 0; index < count; index++ {
		delta := candidate[index].ElapsedMS - baseline[index].ElapsedMS
		total += delta
		if delta < 0 {
			faster++
		}
	}
	return map[string]interface{}{"pairs": count, "averageElapsedDeltaMs": total / int64(count), "candidateFasterPairs": faster}
}

func median(values []int64) int64 {
	return percentile(values, 50)
}

func percentile(values []int64, percent int) int64 {
	if len(values) == 0 {
		return 0
	}
	copyValues := append([]int64(nil), values...)
	sort.Slice(copyValues, func(i, j int) bool { return copyValues[i] < copyValues[j] })
	if percent <= 0 {
		return copyValues[0]
	}
	if percent >= 100 {
		return copyValues[len(copyValues)-1]
	}
	index := (percent*len(copyValues)+99)/100 - 1
	return copyValues[index]
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
