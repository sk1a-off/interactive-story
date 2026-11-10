package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const pairedJudgeContract = `You are a blinded evaluator of Russian interactive fiction. Two continuations A/B were generated from exactly the same story input. You do not know which systems produced them. Judge only supplied context, player attempt and textual evidence. Do not infer provenance or favor length. Do not browse or use tools.
Rate each text from 1 (poor) to 5 (excellent) on continuity, agency, sceneProgress, prose, nonRepetition. Continuity means preserving facts/resources/knowledge and starting after the previous beat; agency means respecting the method/constraints of the attempted action and leaving new strategic choices to the player. SceneProgress needs observable consequences and a new decision, not just paraphrase. Prose should be concrete, readable Russian with purposeful dialogue; do not penalize separate Russian dialogue paragraphs merely for exceeding a paragraph counter. NonRepetition covers redundant recaps and repeated devices.
For each critical factual/agency error, provide a short exact quote. A failed attempt can be a good story outcome. Never require success or reward invented resources. Output ONLY JSON:
{"A":{"continuity":1,"agency":1,"sceneProgress":1,"prose":1,"nonRepetition":1},"B":{"continuity":1,"agency":1,"sceneProgress":1,"prose":1,"nonRepetition":1},"winner":"A|B|tie","criticalErrorsA":[{"type":"continuity|agency","quote":"exact short quote"}],"criticalErrorsB":[],"explanation":"brief Russian assessment with evidence"}`

type judgeScore struct{ Continuity, Agency, SceneProgress, Prose, NonRepetition float64 }
type judgeVerdict struct {
	A, B                             judgeScore
	Winner                           string
	CriticalErrorsA, CriticalErrorsB []struct{ Type, Quote string }
	Explanation                      string
}

func validJudgeVerdict(raw json.RawMessage, a, b string) bool {
	var v judgeVerdict
	if json.Unmarshal(raw, &v) != nil || v.Winner != "A" && v.Winner != "B" && v.Winner != "tie" {
		return false
	}
	for _, s := range []judgeScore{v.A, v.B} {
		for _, x := range []float64{s.Continuity, s.Agency, s.SceneProgress, s.Prose, s.NonRepetition} {
			if x < 1 || x > 5 {
				return false
			}
		}
	}
	for _, errs := range []struct {
		text   string
		errors []struct{ Type, Quote string }
	}{{a, v.CriticalErrorsA}, {b, v.CriticalErrorsB}} {
		for _, e := range errs.errors {
			if e.Quote == "" || !strings.Contains(errs.text, e.Quote) {
				return false
			}
		}
	}
	return true
}
func runPairedJudging(ctx context.Context, root string, m pairedModels, plan pairedPlan, cases []pairedCase) error {
	queue := make(chan pairSpec)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for spec := range queue {
				c, _ := pairedCaseByID(cases, spec.CaseID)
				if !c.Writer {
					continue
				}
				var pair pairedResult
				if loadPaired(filepath.Join(root, "pairs", spec.ID+".json"), &pair) != nil {
					continue
				}
				if !pair.Outcomes["chatgpt"].OK || !pair.Outcomes["antigravity"].OK {
					_ = savePaired(filepath.Join(root, "judgments", spec.ID+"-skipped.json"), map[string]any{"status": "not_eligible_both_outputs_required"})
					continue
				}
				for index, judge := range []string{"chatgpt", "antigravity"} {
					path := filepath.Join(root, "judgments", spec.ID+"-"+judge+".json")
					var old any
					if loadPaired(path, &old) == nil {
						continue
					}
					aArm := spec.AArm
					if index == 1 {
						if aArm == "chatgpt" {
							aArm = "antigravity"
						} else {
							aArm = "chatgpt"
						}
					}
					bArm := "antigravity"
					if aArm == bArm {
						bArm = "chatgpt"
					}
					var a, b struct {
						Text string `json:"text"`
					}
					_ = json.Unmarshal(pair.Outcomes[aArm].Output, &a)
					_ = json.Unmarshal(pair.Outcomes[bArm].Output, &b)
					var judgeContext map[string]any
					_ = json.Unmarshal(c.Input, &judgeContext)
					if context, ok := judgeContext["context"].(map[string]any); ok {
						delete(context, "backgroundFacts")
					}
					input, _ := json.Marshal(map[string]any{"storyInput": judgeContext, "mustPreserve": c.Facts, "A": a.Text, "B": b.Text})
					blind := map[string]any{"case": spec.CaseID, "repeat": spec.Repeat, "input": json.RawMessage(input)}
					_ = savePaired(filepath.Join(root, "blind", fmt.Sprintf("%s-judge%d.json", spec.ID, index+1)), blind)
					var outcome compareOutcome
					if _, e := os.Stat(path + ".started"); e == nil {
						outcome = compareOutcome{Arm: judge, ErrorCode: "interrupted_not_retried"}
					} else {
						_ = savePaired(path+".started", map[string]string{"pair": spec.ID, "judge": judge})
						outcome = compareGenerate(ctx, judge, m.judges[judge], pairedCase{Role: "paired_judge", Input: input})
					}
					valid := outcome.OK && validJudgeVerdict(outcome.Output, a.Text, b.Text)
					_ = savePaired(path, map[string]any{"pair": spec.ID, "judge": judge, "aArm": aArm, "bArm": bArm, "validVerdict": valid, "outcome": outcome})
					fmt.Printf("judge pair=%s judge=%s valid=%t\n", spec.ID, judge, valid)
				}
			}
		}()
	}
	for _, s := range plan.Schedule {
		if ctx.Err() != nil {
			break
		}
		if c, ok := pairedCaseByID(cases, s.CaseID); ok && c.Writer {
			queue <- s
		}
	}
	close(queue)
	wg.Wait()
	if ctx.Err() != nil {
		return fmt.Errorf("judging interrupted")
	}
	return nil
}
