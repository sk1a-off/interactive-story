package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
)

var memoryBenchHeads = map[string]int64{"root": 200, "child": 160, "nested": 150, "restored": 60, "foreign": 200}

func seedMemoryBenchmark(ctx context.Context, tx pgx.Tx, c memoryBenchCorpus, vectors [][]float32, provider ai.ProviderIdentity) error {
	exec := func(sql string, args ...any) error { _, err := tx.Exec(ctx, sql, args...); return err }
	for _, owner := range []string{"owner", "foreign-owner"} {
		if err := exec(`INSERT INTO users(id,username) VALUES($1,$2)`, memoryBenchID(owner), "memorybench-"+owner); err != nil {
			return err
		}
	}
	for _, story := range []struct{ key, owner string }{{"story", "owner"}, {"foreign-story", "foreign-owner"}} {
		if err := exec(`INSERT INTO stories(id,owner_id,title) VALUES($1,$2,'Frozen synthetic memory benchmark')`, memoryBenchID(story.key), memoryBenchID(story.owner)); err != nil {
			return err
		}
	}
	for _, root := range []struct{ key, story string }{{"root", "story"}, {"foreign", "foreign-story"}} {
		if err := exec(`INSERT INTO timelines(id,story_id,name,head_event_seq) VALUES($1,$2,$3,200)`, memoryBenchID(root.key), memoryBenchID(root.story), root.key); err != nil {
			return err
		}
	}
	for _, fork := range []struct {
		child, parent string
		at            int64
	}{{"child", "root", 100}, {"nested", "child", 130}, {"restored", "root", 60}} {
		snap, save := memoryBenchID("snapshot/"+fork.child), memoryBenchID("save/"+fork.child)
		if err := exec(`INSERT INTO snapshots(id,timeline_id,event_seq,state,state_hash) VALUES($1,$2,$3,'{}',repeat('0',64))`, snap, memoryBenchID(fork.parent), fork.at); err != nil {
			return err
		}
		if err := exec(`INSERT INTO save_points(id,story_id,timeline_id,snapshot_id,event_seq,name) VALUES($1,$2,$3,$4,$5,'benchmark fork')`, save, memoryBenchID("story"), memoryBenchID(fork.parent), snap, fork.at); err != nil {
			return err
		}
		if err := exec(`INSERT INTO timelines(id,story_id,parent_timeline_id,forked_from_save_id,name,head_event_seq) VALUES($1,$2,$3,$4,$5,$6)`, memoryBenchID(fork.child), memoryBenchID("story"), memoryBenchID(fork.parent), save, fork.child, fork.at); err != nil {
			return err
		}
		if err := exec(`UPDATE timelines SET head_event_seq=$2 WHERE id=$1`, memoryBenchID(fork.child), memoryBenchHeads[fork.child]); err != nil {
			return err
		}
	}
	for name, head := range memoryBenchHeads {
		story := "story"
		if name == "foreign" {
			story = "foreign-story"
		}
		if err := exec(`INSERT INTO memory_index_state(timeline_id,story_id,observed_event_seq) VALUES($1,$2,$3) ON CONFLICT(timeline_id) DO UPDATE SET observed_event_seq=EXCLUDED.observed_event_seq`, memoryBenchID(name), memoryBenchID(story), head); err != nil {
			return err
		}
		if err := exec(`INSERT INTO memory_index_publication_state(timeline_id,provider_name,model_name,embedding_revision,published_event_seq) VALUES($1,$2,$3,1,$4)`, memoryBenchID(name), provider.Provider, provider.Model, head); err != nil {
			return err
		}
	}
	for i, d := range c.Documents {
		if _, ok := memoryBenchHeads[d.Timeline]; !ok {
			return fmt.Errorf("unknown fixture timeline")
		}
		story := "story"
		if d.Timeline == "foreign" {
			story = "foreign-story"
		}
		ownerType, owner := memory.OwnerTimeline, memoryBenchID(d.Timeline)
		if d.Owner != "" && d.Owner != "timeline" {
			ownerType, owner = memory.OwnerCharacter, memoryBenchID("character/"+d.Owner)
		}
		model, revision := provider.Model, int64(1)
		if d.Profile == "other-model" {
			model += "/other"
		}
		if d.Profile == "other-revision" {
			revision = 2
		}
		var class any = d.Class
		if d.Class == "" {
			class = nil
		}
		vector, _ := json.Marshal(vectors[i])
		if err := exec(`INSERT INTO memory_embeddings(id,story_id,timeline_id,owner_type,owner_id,kind,content,embedding,embedding_provider,embedding_model,embedding_revision,valid_from_event_seq,valid_to_event_seq,source_key,document_class)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8::vector,$9,$10,$11,$12,$13,$14,$15)`, memoryBenchID("document/"+d.ID), memoryBenchID(story), memoryBenchID(d.Timeline), string(ownerType), owner, d.Kind, d.Text, string(vector), provider.Provider, model, revision, d.From, d.To, d.ID, class); err != nil {
			return err
		}
	}
	return nil
}
func setMemoryBenchPublication(ctx context.Context, tx pgx.Tx, s memoryBenchScope, p ai.ProviderIdentity) error {
	head, ok := memoryBenchHeads[s.Timeline]
	if !ok {
		return nil
	}
	if s.Lag {
		head--
	}
	_, err := tx.Exec(ctx, `UPDATE memory_index_publication_state SET published_event_seq=$4 WHERE timeline_id=$1 AND provider_name=$2 AND model_name=$3 AND embedding_revision=1`, memoryBenchID(s.Timeline), p.Provider, p.Model, head)
	return err
}
func memoryBenchmarkQuery(s memoryBenchScope, text string, vector []float32, p ai.ProviderIdentity, k int) memory.Query {
	story := "story"
	if s.Timeline == "foreign" || s.ForeignStory {
		story = "foreign-story"
	}
	ownerType, owner := memory.OwnerTimeline, memoryBenchID(s.Timeline)
	kinds := []string{"beat", "fact", "thread", "chapter_summary"}
	if s.Owner != "" && s.Owner != "timeline" {
		ownerType, owner = memory.OwnerCharacter, memoryBenchID("character/"+s.Owner)
		kinds = []string{"knowledge", "belief"}
	}
	return memory.Query{StoryID: memoryBenchID(story), TimelineID: timeline.ID(memoryBenchID(s.Timeline)), OwnerType: ownerType, OwnerID: id.ID(owner), AtEventSeq: s.At, Vector: vector, Text: text, Limit: k, Kinds: kinds, Profile: memory.EmbeddingProfile{Provider: p.Provider, Model: p.Model, Revision: 1}}
}
