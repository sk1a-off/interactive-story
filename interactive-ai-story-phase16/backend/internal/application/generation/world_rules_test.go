package generation

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

type sequentialLoreLLM struct {
	mu        sync.Mutex
	responses map[string][][]byte
	calls     map[string]int
}

func (l *sequentialLoreLLM) Identity() aiport.ProviderIdentity {
	return aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "fake", Model: "lore-sequence", Profile: "test"}
}

func (l *sequentialLoreLLM) Generate(_ context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	index := l.calls[request.Role]
	l.calls[request.Role] = index + 1
	values := l.responses[request.Role]
	if len(values) == 0 {
		return aiport.StoryResponse{}, aiport.ErrInvalidOutput
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return aiport.StoryResponse{Output: append([]byte(nil), values[index]...)}, nil
}

type memoryRuleAudit struct {
	records []generationtarget.RuleAuditRecord
}

func (m *memoryRuleAudit) RecordRuleAudit(_ context.Context, _ timeline.ID, _ id.ID, records []generationtarget.RuleAuditRecord) error {
	m.records = append(m.records, records...)
	return nil
}

func loreTarget() generationtarget.Target {
	return generationtarget.Target{
		SceneID:          id.MustParse("00000000-0000-4000-8000-000000000090"),
		NextBeatPosition: 2,
		WorldSystems:     []generationtarget.WorldSystem{{ID: "magic", Name: "Магия", Kind: "magic", Status: "active"}},
		WorldRules: []generationtarget.WorldRule{{
			ID: "MAGIC-001", SystemID: "magic", Title: "Цена заклинания", Severity: "hard",
			Statement: "Любое заклинание расходует фокус до появления эффекта.",
			Costs:     []string{"минимум одна единица фокуса"}, ForbiddenResults: []string{"бесплатное заклинание"},
			Tags: []string{"магия", "заклинание", "фокус"}, Visibility: "known_to_hero", Status: "established",
		}},
		WorldResources: []generationtarget.WorldResource{{ID: "magic.focus", SystemID: "magic", OwnerType: "hero", Name: "Фокус", Current: 3, Visibility: "known_to_hero"}},
	}
}

func lorePipelineResponses(writer, lore [][]byte) map[string][][]byte {
	base := scripted().Responses
	responses := make(map[string][][]byte, len(base)+1)
	for role, output := range base {
		responses[role] = [][]byte{output}
	}
	responses["writer"] = writer
	responses["lore_guard"] = lore
	return responses
}

func TestLoreGuardRepairsHardViolationBeforeCanon(t *testing.T) {
	bad := []byte(`{"text":"Герой без усилий создаёт заклинание, не расходуя фокус."}`)
	good := []byte(`{"text":"Герой собирает волю в узел. Индикатор фокуса падает с трёх до двух, и только после этого заклинание зажигает руны на двери."}`)
	violation := []byte(`{"valid":false,"violations":[{"ruleId":"MAGIC-001","evidence":"Заклинание создано без расхода фокуса.","repairInstruction":"Показать списание фокуса до эффекта."}],"ruleUses":["MAGIC-001"]}`)
	valid := []byte(`{"valid":true,"violations":[],"ruleUses":["MAGIC-001"]}`)
	llm := &sequentialLoreLLM{responses: lorePipelineResponses([][]byte{bad, good}, [][]byte{violation, valid}), calls: map[string]int{}}
	canonStore := &appender{}
	audit := &memoryRuleAudit{}
	pipeline := Pipeline{LLM: llm, Canon: canonStore, Targets: staticTarget{target: loreTarget()}, RuleAudits: audit}
	events, err := pipeline.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000071"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000072")), Text: "Я применяю заклинание к двери"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 2 || events[1].Type != "beat_committed" {
		t.Fatalf("repaired beat was not committed: %+v", events)
	}
	if len(audit.records) != 1 || audit.records[0].Status != "repaired" || audit.records[0].RuleID != "MAGIC-001" {
		t.Fatalf("repair audit was not preserved: %+v", audit.records)
	}
	if llm.calls["writer"] != 2 || llm.calls["lore_guard"] != 2 {
		t.Fatalf("expected one bounded rewrite and recheck, calls=%+v", llm.calls)
	}
}

func TestLoreGuardBlocksBeatWhenHardViolationRemains(t *testing.T) {
	bad := []byte(`{"text":"Герой снова создаёт бесплатное заклинание, и дверь исчезает без всякой цены."}`)
	violation := []byte(`{"valid":false,"violations":[{"ruleId":"MAGIC-001","evidence":"Эффект снова не расходует фокус.","repairInstruction":"Списать фокус до эффекта."}],"ruleUses":["MAGIC-001"]}`)
	llm := &sequentialLoreLLM{responses: lorePipelineResponses([][]byte{bad, bad}, [][]byte{violation, violation}), calls: map[string]int{}}
	canonStore := &appender{}
	audit := &memoryRuleAudit{}
	pipeline := Pipeline{LLM: llm, Canon: canonStore, Targets: staticTarget{target: loreTarget()}, RuleAudits: audit}
	_, err := pipeline.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000073"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000074")), Text: "Я колдую бесплатно"})
	if !errors.Is(err, ErrWorldRuleViolation) {
		t.Fatalf("hard contradiction must block Canon, got %v", err)
	}
	if len(canonStore.events) != 0 {
		t.Fatalf("a blocked draft entered Canon: %+v", canonStore.events)
	}
	if len(audit.records) != 1 || audit.records[0].Status != "blocking" {
		t.Fatalf("blocking audit missing: %+v", audit.records)
	}
}

func TestGeneratedHardRuleStaysPending(t *testing.T) {
	target := loreTarget()
	events, err := validateWorldRuleChanges(target, []worldRuleChange{{Operation: "propose_rule", SystemID: "magic", Title: "Новый предел", Category: "limit", Severity: "hard", Statement: "Один маг поддерживает только одно активное поле.", Evidence: "В сцене второе поле разрушило первое."}}, nil, false)
	if err != nil || len(events) != 1 || events[0].Type != "world_rule_upserted" {
		t.Fatalf("hard rule proposal failed: events=%+v err=%v", events, err)
	}
	var payload struct {
		Status string `json:"status"`
	}
	if err = json.Unmarshal(events[0].Payload, &payload); err != nil || payload.Status != "pending" {
		t.Fatalf("generated hard rule must await approval: %s", events[0].Payload)
	}
}
