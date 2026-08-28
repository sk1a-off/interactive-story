package generation

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

type loreViolation struct {
	RuleID            string `json:"ruleId"`
	Severity          string `json:"severity"`
	Evidence          string `json:"evidence"`
	RepairInstruction string `json:"repairInstruction"`
}
type loreCheck struct {
	Valid      bool            `json:"valid"`
	Violations []loreViolation `json:"violations"`
	RuleUses   []string        `json:"ruleUses"`
}
type worldRuleChange struct {
	Operation        string   `json:"operation"`
	RuleID           string   `json:"ruleId,omitempty"`
	SystemID         string   `json:"systemId,omitempty"`
	Title            string   `json:"title,omitempty"`
	Category         string   `json:"category,omitempty"`
	Severity         string   `json:"severity,omitempty"`
	Statement        string   `json:"statement,omitempty"`
	Preconditions    []string `json:"preconditions,omitempty"`
	Costs            []string `json:"costs,omitempty"`
	ForbiddenResults []string `json:"forbiddenResults,omitempty"`
	Exceptions       []string `json:"exceptions,omitempty"`
	Tags             []string `json:"tags,omitempty"`
	Visibility       string   `json:"visibility,omitempty"`
	ExceptionOf      string   `json:"exceptionOf,omitempty"`
	Evidence         string   `json:"evidence,omitempty"`
}
type worldResourceChange struct {
	ResourceID string  `json:"resourceId"`
	Delta      float64 `json:"delta"`
	Evidence   string  `json:"evidence"`
}

func relevantWorldRules(target generationtarget.Target, texts ...string) []generationtarget.WorldRule {
	query := tokenSet(strings.Join(texts, " "))
	type scored struct {
		rule  generationtarget.WorldRule
		score int
	}
	values := make([]scored, 0, len(target.WorldRules))
	for _, rule := range target.WorldRules {
		if rule.Status != "established" {
			continue
		}
		score := 0
		if rule.Severity == "hard" {
			score += 100
		}
		if rule.Visibility == "hidden" || rule.Severity == "mystery" {
			score += 10
		}
		for token := range tokenSet(rule.ID + " " + rule.SystemID + " " + rule.Title + " " + rule.Statement + " " + strings.Join(rule.Tags, " ")) {
			if query[token] {
				score += 4
			}
		}
		for _, resource := range target.WorldResources {
			if resource.SystemID == rule.SystemID {
				for token := range tokenSet(resource.Name + " " + resource.ID) {
					if query[token] {
						score += 3
					}
				}
			}
		}
		if score > 0 {
			values = append(values, scored{rule: rule, score: score})
		}
	}
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].score == values[j].score {
			return values[i].rule.ID < values[j].rule.ID
		}
		return values[i].score > values[j].score
	})
	if len(values) > 24 {
		values = values[:24]
	}
	out := make([]generationtarget.WorldRule, 0, len(values))
	for _, value := range values {
		out = append(out, value.rule)
	}
	return out
}

func tokenSet(value string) map[string]bool {
	returnValue := map[string]bool{}
	var b strings.Builder
	flush := func() {
		if b.Len() >= 3 {
			returnValue[strings.ToLower(b.String())] = true
		}
		b.Reset()
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return returnValue
}

func worldFeasibility(rules []generationtarget.WorldRule, resources []generationtarget.WorldResource) map[string]any {
	hard, costs, forbidden := []string{}, []string{}, []string{}
	for _, rule := range rules {
		if rule.Severity == "hard" {
			hard = append(hard, rule.ID+": "+rule.Statement)
		}
		for _, cost := range rule.Costs {
			costs = append(costs, rule.ID+": "+cost)
		}
		for _, result := range rule.ForbiddenResults {
			forbidden = append(forbidden, rule.ID+": "+result)
		}
	}
	return map[string]any{"hardConstraints": hard, "requiredCosts": costs, "forbiddenResults": forbidden, "resources": resources, "instruction": "Treat constraints as outcome boundaries. The attempted action may fail or have a cost; never grant an impossible result."}
}

func validateLoreCheck(check loreCheck, relevant []generationtarget.WorldRule) (loreCheck, error) {
	known := map[string]generationtarget.WorldRule{}
	for _, rule := range relevant {
		known[rule.ID] = rule
	}
	uses := make([]string, 0, len(check.RuleUses))
	seen := map[string]bool{}
	for _, ruleID := range check.RuleUses {
		if _, ok := known[ruleID]; ok && !seen[ruleID] {
			seen[ruleID] = true
			uses = append(uses, ruleID)
		}
	}
	check.RuleUses = uses
	validViolations := make([]loreViolation, 0, len(check.Violations))
	for _, violation := range check.Violations {
		rule, ok := known[violation.RuleID]
		if !ok || strings.TrimSpace(violation.Evidence) == "" {
			continue
		}
		violation.Severity = rule.Severity
		if violation.Severity == "mystery" || violation.Severity == "belief" {
			violation.Severity = "soft"
		}
		if strings.TrimSpace(violation.RepairInstruction) == "" {
			violation.RepairInstruction = "Rewrite the contradicted outcome so it follows rule " + rule.ID
		}
		validViolations = append(validViolations, violation)
	}
	check.Violations = validViolations
	check.Valid = len(validViolations) == 0
	return check, nil
}

func loreRequiresRewrite(check loreCheck, safe bool) bool {
	for _, v := range check.Violations {
		if v.Severity == "hard" || safe {
			return true
		}
	}
	return false
}

func ruleAuditRecords(check loreCheck, status string) []generationtarget.RuleAuditRecord {
	out := make([]generationtarget.RuleAuditRecord, 0, len(check.Violations))
	for _, v := range check.Violations {
		out = append(out, generationtarget.RuleAuditRecord{RuleID: v.RuleID, Severity: v.Severity, Evidence: v.Evidence, RepairInstruction: v.RepairInstruction, Status: status})
	}
	return out
}

func validateWorldRuleChanges(target generationtarget.Target, changes []worldRuleChange, resources []worldResourceChange, safe bool) ([]proposedEvent, error) {
	knownRules := map[string]generationtarget.WorldRule{}
	for _, rule := range target.WorldRules {
		knownRules[rule.ID] = rule
	}
	knownSystems := map[string]bool{}
	for _, system := range target.WorldSystems {
		knownSystems[system.ID] = true
	}
	knownResources := map[string]generationtarget.WorldResource{}
	for _, resource := range target.WorldResources {
		knownResources[resource.ID] = resource
	}
	events := []proposedEvent{}
	for _, change := range changes {
		change.Operation = strings.ToLower(strings.TrimSpace(change.Operation))
		change.Evidence = strings.TrimSpace(change.Evidence)
		switch change.Operation {
		case "reveal":
			rule, ok := knownRules[change.RuleID]
			if !ok || change.Evidence == "" {
				continue
			}
			rule.Visibility = "known_to_hero"
			rule.Evidence = change.Evidence
			rule.Version++
			events = append(events, proposedEvent{Type: "world_rule_upserted", Payload: worldRulePayload(rule)})
		case "propose_rule", "add_exception":
			if !knownSystems[change.SystemID] || strings.TrimSpace(change.Title) == "" || strings.TrimSpace(change.Statement) == "" || change.Evidence == "" {
				continue
			}
			if change.Severity != "hard" && change.Severity != "soft" && change.Severity != "mystery" && change.Severity != "belief" {
				change.Severity = "soft"
			}
			if change.Category == "" {
				change.Category = "law"
			}
			if change.Operation == "add_exception" {
				change.Category = "exception"
				if _, ok := knownRules[change.ExceptionOf]; !ok {
					continue
				}
			}
			ruleID := strings.TrimSpace(change.RuleID)
			if ruleID == "" {
				fresh, err := id.New()
				if err != nil {
					return nil, err
				}
				ruleID = "PROPOSED-" + strings.ToUpper(strings.ReplaceAll(fresh.String()[:8], "-", ""))
			}
			if _, exists := knownRules[ruleID]; exists {
				continue
			}
			status := "established"
			if safe || change.Severity == "hard" || change.Operation == "add_exception" {
				status = "pending"
			}
			rule := generationtarget.WorldRule{ID: ruleID, SystemID: change.SystemID, Title: change.Title, Category: change.Category, Severity: change.Severity, Statement: change.Statement, Preconditions: change.Preconditions, Costs: change.Costs, ForbiddenResults: change.ForbiddenResults, Exceptions: change.Exceptions, Tags: change.Tags, Visibility: change.Visibility, Status: status, ExceptionOf: change.ExceptionOf, Source: "generation", Evidence: change.Evidence, Version: 1}
			if rule.Visibility == "" {
				rule.Visibility = "canon_only"
			}
			events = append(events, proposedEvent{Type: "world_rule_upserted", Payload: worldRulePayload(rule)})
		}
	}
	for _, change := range resources {
		current, ok := knownResources[change.ResourceID]
		if !ok || strings.TrimSpace(change.Evidence) == "" || change.Delta == 0 {
			continue
		}
		next := current.Current + change.Delta
		if (current.Minimum != nil && next < *current.Minimum) || (current.Maximum != nil && next > *current.Maximum) {
			return nil, fmt.Errorf("%w: resource %s outside bounds", ErrRejectedProposal, current.ID)
		}
		current.Current = next
		current.Version++
		events = append(events, proposedEvent{Type: "world_resource_changed", Payload: mustJSON(map[string]any{"resourceId": current.ID, "systemId": current.SystemID, "ownerType": current.OwnerType, "ownerId": current.OwnerID, "ownerKey": current.OwnerKey, "name": current.Name, "unit": current.Unit, "currentValue": current.Current, "minValue": current.Minimum, "maxValue": current.Maximum, "visibility": current.Visibility, "version": current.Version, "evidence": change.Evidence})})
	}
	return events, nil
}

func worldRulePayload(rule generationtarget.WorldRule) json.RawMessage {
	return mustJSON(map[string]any{"ruleId": rule.ID, "systemId": rule.SystemID, "title": rule.Title, "category": rule.Category, "severity": rule.Severity, "statement": rule.Statement, "preconditions": rule.Preconditions, "costs": rule.Costs, "forbiddenResults": rule.ForbiddenResults, "exceptions": rule.Exceptions, "tags": rule.Tags, "visibility": rule.Visibility, "status": rule.Status, "exceptionOf": rule.ExceptionOf, "source": rule.Source, "evidence": rule.Evidence, "version": rule.Version})
}
