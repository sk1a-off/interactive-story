package generation

import (
	"context"
	_ "embed"
	"encoding/json"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
	"testing"
)

//go:embed testdata/journal-isolation-frozen.json
var frozenJournalIsolation []byte

func BenchmarkFrozenJournalIsolation(b *testing.B) {
	var fixtures []struct {
		ID      string
		Text    string
		Journal []generationtarget.JournalEntry
		State   json.RawMessage
	}
	if err := json.Unmarshal(frozenJournalIsolation, &fixtures); err != nil {
		b.Fatal(err)
	}
	for _, f := range fixtures {
		for _, enabled := range []bool{false, true} {
			label := "off"
			if enabled {
				label = "on"
			}
			b.Run(f.ID+"/"+label, func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					_, _ = checkJournalProposal(f.Text, f.Journal, f.State, enabled)
				}
			})
		}
	}
}

func TestFrozenJournalIsolationThroughPipeline(t *testing.T) {
	var fixtures []struct {
		ID      string
		Text    string
		Journal []generationtarget.JournalEntry
		State   json.RawMessage
	}
	if err := json.Unmarshal(frozenJournalIsolation, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		for _, enabled := range []bool{false, true} {
			llm := scripted()
			llm.Responses["writer"], _ = json.Marshal(map[string]string{"text": f.Text})
			llm.Responses["state_evaluator"] = f.State
			c := &appender{}
			p := Pipeline{LLM: llm, Canon: c, Targets: staticTarget{target: generationtarget.Target{SceneID: id.MustParse("00000000-0000-4000-8000-000000000090"), NextBeatPosition: 2, Journal: f.Journal}}, JournalEvidenceIsolationV1: enabled}
			events, err := p.Run(context.Background(), mustID(6), PlayerAction{TimelineID: timeline.ID(mustID(7)), Text: "Завершить действие"})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, e := range events {
				if e.Type == "journal_entry_created" {
					t.Fatal("bad key survived")
				}
				if e.Type == "journal_entry_updated" {
					count++
					var value struct {
						Quantity int
						Status   string
					}
					if err := json.Unmarshal(e.Payload, &value); err != nil {
						t.Fatal(err)
					}
					if f.ID == "spend-coins-01" && (value.Quantity != 7 || value.Status != "active") {
						t.Fatalf("wrong balance: %s", e.Payload)
					}
					if f.ID == "consume-last-ration-01" && (value.Quantity != 0 || value.Status != "inactive") {
						t.Fatalf("wrong ration: %s", e.Payload)
					}
				}
			}
			want := 1
			if !enabled && f.ID == "spend-coins-01" {
				want = 0
			}
			if count != want {
				t.Fatalf("case=%s enabled=%v got=%d want=%d", f.ID, enabled, count, want)
			}
			if events[len(events)-1].Type != "choices_ready" {
				t.Fatal("choices not committed after journal")
			}
		}
	}
}

func TestJournalIsolationBoundaries(t *testing.T) {
	const coinID = "00000000-0000-4000-8000-000000000101"
	current := []generationtarget.JournalEntry{{ID: coinID, Category: "currency", Name: "Монеты", Quantity: 10, Status: "active"}}
	beat := "Осталось семь монет. На стойку положили ключ. Алекс забрал ключ."
	coin := journalChange{Operation: "update", EntryID: coinID, Quantity: intPointer(7), EvidenceQuote: "Осталось семь монет."}
	key := journalChange{Operation: "create", Category: "item", Name: "Ключ", EvidenceQuote: "Со стойкой положили ключ."}
	clone := func(c journalChange, mutate func(*journalChange)) journalChange { mutate(&c); return c }
	for _, tc := range []struct {
		name    string
		changes []journalChange
		want    bool
	}{
		{"independent balance", []journalChange{coin, key}, true},
		{"reversed order", []journalChange{key, coin}, true},
		{"duplicate update", []journalChange{coin, coin, key}, false},
		{"create references existing", []journalChange{coin, clone(key, func(c *journalChange) { c.EntryID = coinID })}, false},
		{"unknown target", []journalChange{clone(coin, func(c *journalChange) { c.EntryID = "unknown" }), key}, false},
		{"remove dependency", []journalChange{clone(coin, func(c *journalChange) { c.Operation = "remove" }), key}, false},
		{"overlapping evidence", []journalChange{coin, clone(key, func(c *journalChange) {
			c.EvidenceQuote = coin.EvidenceQuote + " Затем появился ключ."
		})}, false},
		{"missing evidence", []journalChange{coin, clone(key, func(c *journalChange) { c.EvidenceQuote = "" })}, false},
		{"duplicate create", []journalChange{coin, key, key}, false},
		{"negative balance", []journalChange{clone(coin, func(c *journalChange) { c.Quantity = intPointer(-1) }), key}, false},
		{"bad balance quote", []journalChange{clone(coin, func(c *journalChange) { c.EvidenceQuote = "Осталось шесть монет." }), key}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"changes": tc.changes})
			normalized, err := CheckIsolatedJournalProposal(beat, current, raw)
			if (err == nil) != tc.want {
				t.Fatalf("result=%s err=%v", normalized, err)
			}
			if tc.want {
				var got []journalChange
				if json.Unmarshal(normalized, &got) != nil || len(got) != 1 || got[0].EntryID != coinID || got[0].Quantity == nil || *got[0].Quantity != 7 {
					t.Fatalf("wrong salvaged state: %s", normalized)
				}
				if _, err := CheckJournalProposal(beat, current, raw); err == nil {
					t.Fatal("default gate changed")
				}
			}
		})
	}
}
