package postgres_test

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	pg "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/application/contextbuilder"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
)

type hybridFixture struct {
	t           *testing.T
	ctx         context.Context
	tx          pgx.Tx
	story, root id.ID
}

func newHybridFixture(t *testing.T) *hybridFixture {
	t.Helper()
	ctx, tx, story, root := memoryOutboxFixture(t)
	return &hybridFixture{t: t, ctx: ctx, tx: tx, story: story, root: root}
}

func (f *hybridFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.tx.Exec(f.ctx, sql, args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *hybridFixture) fork(parent id.ID, seq, head int64) id.ID {
	f.t.Helper()
	snap, _ := id.New()
	save, _ := id.New()
	child, _ := id.New()
	f.exec(`INSERT INTO snapshots(id,timeline_id,event_seq,state,state_hash) VALUES($1,$2,$3,'{}',repeat('0',64))`, snap, parent, seq)
	f.exec(`INSERT INTO save_points(id,story_id,timeline_id,snapshot_id,event_seq,name) VALUES($1,$2,$3,$4,$5,'hybrid fixture')`, save, f.story, parent, snap, seq)
	f.exec(`INSERT INTO timelines(id,story_id,parent_timeline_id,forked_from_save_id,name,head_event_seq) VALUES($1,$2,$3,$4,'hybrid child',$5)`, child, f.story, parent, save, seq)
	f.exec(`UPDATE timelines SET head_event_seq=$2 WHERE id=$1`, child, head)
	return child
}

func (f *hybridFixture) document(tid id.ID, key, class, content string, from int64, until *int64, vector string) id.ID {
	f.t.Helper()
	doc, _ := id.New()
	f.exec(`INSERT INTO memory_embeddings(id,story_id,timeline_id,owner_type,owner_id,kind,content,embedding,
 embedding_provider,embedding_model,embedding_revision,valid_from_event_seq,valid_to_event_seq,source_key,document_class)
 VALUES($1,$2,$3,'timeline',$3,'fixture',$4,$5::vector,'test','384',1,$6,$7,$8,$9)`, doc, f.story, tid, content, vector, from, until, key, class)
	return doc
}

func (f *hybridFixture) publish(tid id.ID, observed, published int64) {
	f.t.Helper()
	f.exec(`INSERT INTO memory_index_state(story_id,timeline_id,observed_event_seq) VALUES($1,$2,$3)
 ON CONFLICT(timeline_id) DO UPDATE SET observed_event_seq=EXCLUDED.observed_event_seq`, f.story, tid, observed)
	f.exec(`INSERT INTO memory_index_publication_state(timeline_id,provider_name,model_name,embedding_revision,published_event_seq)
 VALUES($1,'test','384',1,$2) ON CONFLICT(timeline_id,provider_name,model_name,embedding_revision)
 DO UPDATE SET published_event_seq=EXCLUDED.published_event_seq`, tid, published)
}

func hybridVector(x, y float32) string {
	v := make([]string, 384)
	for i := range v {
		v[i] = "0"
	}
	v[0], v[1] = fmt.Sprint(x), fmt.Sprint(y)
	return "[" + strings.Join(v, ",") + "]"
}

func (f *hybridFixture) query(tid id.ID, at int64) memory.Query {
	v := make([]float32, 384)
	v[0] = 1
	return memory.Query{StoryID: f.story, TimelineID: timeline.ID(tid), OwnerType: memory.OwnerTimeline, OwnerID: tid,
		AtEventSeq: at, Vector: v, Text: "драконом", Limit: 100, Profile: memory.EmbeddingProfile{Provider: "test", Model: "384", Revision: 1}}
}

func (f *hybridFixture) search(q memory.Query) []memory.Memory {
	f.t.Helper()
	result, err := pg.NewHybridMemoryRetriever(f.tx).Search(f.ctx, q)
	if err != nil {
		f.t.Fatal(err)
	}
	return result
}

func memoryKeys(rows []memory.Memory) []string {
	keys := make([]string, len(rows))
	for i, row := range rows {
		keys[i] = row.SourceKey
	}
	return keys
}

func TestHybridSearchFiltersNestedBranchesBeforeRanking(t *testing.T) {
	f := newHybridFixture(t)
	f.exec(`UPDATE timelines SET head_event_seq=200 WHERE id=$1`, f.root)
	child := f.fork(f.root, 100, 160)
	nested := f.fork(child, 130, 150)
	sibling := f.fork(f.root, 60, 60)
	v := hybridVector(1, 0)
	want := map[string]id.ID{"root-history": f.root, "child-history": child, "local-history": nested, "local-state": nested}
	for _, d := range []struct {
		tid        id.ID
		key, class string
		seq        int64
	}{
		{f.root, "root-history", "historical_beat", 100}, {f.root, "parent-future", "historical_beat", 101},
		{child, "child-history", "historical_beat", 130}, {child, "child-future", "historical_beat", 131},
		{nested, "local-history", "historical_beat", 140}, {nested, "local-future", "historical_beat", 141},
		{sibling, "sibling", "historical_beat", 60}, {f.root, "ancestor-state", "branch_state", 100},
		{child, "parent-state", "branch_state", 130}, {nested, "local-state", "branch_state", 140},
		{nested, "before-branch-base", "branch_state", 129},
	} {
		f.document(d.tid, d.key, d.class, "дракон "+d.key, d.seq, nil, v)
	}
	closed := int64(139)
	f.document(nested, "invalidated", "branch_state", "дракон", 130, &closed, v)
	for _, column := range []string{"embedding_provider", "embedding_model", "embedding_revision"} {
		doc := f.document(nested, column, "historical_beat", "дракон", 140, nil, v)
		value := "'other'"
		if column == "embedding_revision" {
			value = "2"
		}
		f.exec(`UPDATE memory_embeddings SET `+column+`=`+value+` WHERE id=$1`, doc)
	}
	legacy := f.document(nested, "legacy", "branch_state", "дракон", 140, nil, v)
	f.exec(`UPDATE memory_embeddings SET document_class=NULL WHERE id=$1`, legacy)
	private := f.document(nested, "other-owner", "branch_state", "дракон", 140, nil, v)
	character, _ := id.New()
	f.exec(`UPDATE memory_embeddings SET owner_type='character',owner_id=$2 WHERE id=$1`, private, character)
	foreignStory, _ := id.New()
	foreignTimeline, _ := id.New()
	f.exec(`INSERT INTO stories(id,owner_id,title) SELECT $1,owner_id,'foreign hybrid' FROM stories WHERE id=$2`, foreignStory, f.story)
	f.exec(`INSERT INTO timelines(id,story_id,name,head_event_seq) VALUES($1,$2,'foreign',140)`, foreignTimeline, foreignStory)
	foreign := f.document(nested, "foreign-story", "historical_beat", "дракон", 140, nil, v)
	f.exec(`UPDATE memory_embeddings SET story_id=$2,timeline_id=$3,owner_id=$3 WHERE id=$1`, foreign, foreignStory, foreignTimeline)
	f.publish(nested, 140, 140)
	// Evaluate inherited validity at the ancestor's cap (100), not 140.
	f.exec(`UPDATE memory_embeddings SET valid_to_event_seq=100 WHERE timeline_id=$1 AND source_key='root-history'`, f.root)
	q := f.query(nested, 140)
	rows := f.search(q)
	if len(rows) != len(want) {
		t.Fatalf("unexpected candidates: %v", memoryKeys(rows))
	}
	for _, row := range rows {
		if source, ok := want[row.SourceKey]; !ok || row.TimelineID != timeline.ID(source) || row.OwnerID != source {
			t.Fatalf("scope/provenance leak: %+v", row)
		}
	}
	embeddings := fakeai.NewEmbeddingProvider(384)
	embeddings.ID.Provider, embeddings.ID.Model = q.Profile.Provider, q.Profile.Model
	builder := contextbuilder.Builder{Embeddings: embeddings, Memory: pg.NewHybridMemoryRetriever(f.tx), Visibility: pg.NewMemoryVisibilityResolver(f.tx), Profile: q.Profile}
	memoryBudget := 4000
	built, err := builder.Build(f.ctx, contextbuilder.Input{State: contextbuilder.AuthoritativeState{
		StoryID: f.story, TimelineID: timeline.ID(nested), HeadEventSeq: 140, CurrentState: []byte(`{"door":"locked"}`), RecentBeats: []string{"recent"}},
		QueryText: q.Text, OwnerType: q.OwnerType, OwnerID: q.OwnerID, TokenBudget: 10000, RetrievalLimit: 8, MemoryTokenBudget: &memoryBudget})
	if err != nil || built.RetrievalError != "" || len(built.Retrieved) != len(want) {
		t.Fatalf("Context Builder integration: %+v %v", built, err)
	}
	for _, row := range built.Retrieved {
		if row.TimelineID != timeline.ID(want[row.SourceKey]) {
			t.Fatal("lost source provenance")
		}
	}
	q.Limit = 1
	if rows = f.search(q); len(rows) != 1 || want[rows[0].SourceKey] == "" {
		t.Fatalf("top-k leak: %+v", rows)
	}
	// Safe restore sees only ancestor history through the old save.
	rows = f.search(f.query(sibling, 60))
	if len(rows) != 0 {
		t.Fatalf("restore leaked post-save history: %v", memoryKeys(rows))
	}
	for _, invalid := range []memory.Query{f.query(nested, 129), f.query(nested, 151), f.query(foreignTimeline, 140)} {
		if _, err := pg.NewHybridMemoryRetriever(f.tx).Search(f.ctx, invalid); err == nil {
			t.Fatal("invalid lineage request accepted")
		}
	}
}

func TestHybridSearchSuppressesStateDuringIndexLagAndHonorsIntervals(t *testing.T) {
	f := newHybridFixture(t)
	v := hybridVector(1, 0)
	one := int64(1)
	f.document(f.root, "old", "branch_state", "дракон", 0, &one, v)
	f.document(f.root, "current", "branch_state", "дракон", 2, nil, v)
	f.document(f.root, "history", "historical_beat", "дракон", 1, nil, v)
	f.publish(f.root, 2, 2)
	for at, wanted := range map[int64]string{1: "old", 2: "current"} {
		rows := f.search(f.query(f.root, at))
		keys := memoryKeys(rows)
		if len(rows) != 2 || !containsKey(keys, wanted) || !containsKey(keys, "history") {
			t.Fatalf("at %d: %v", at, keys)
		}
	}
	// A real Canon insertion invalidates old structured search immediately,
	// before the asynchronous publisher has run.
	f.exec(`UPDATE memory_index_publication_state SET published_event_seq=1 WHERE timeline_id=$1`, f.root)
	f.exec(`INSERT INTO story_events(timeline_id,seq,event_type,payload) VALUES($1,2,'player_action_attempted','{}')`, f.root)
	if keys := memoryKeys(f.search(f.query(f.root, 2))); !reflect.DeepEqual(keys, []string{"history"}) {
		t.Fatalf("stale state: %v", keys)
	}
	f.exec(`INSERT INTO memory_index_publication_state VALUES($1,'test','384',2,200)`, f.root)
	if keys := memoryKeys(f.search(f.query(f.root, 2))); !reflect.DeepEqual(keys, []string{"history"}) {
		t.Fatalf("another revision unblocked state: %v", keys)
	}
	f.publish(f.root, 2, 2)
	if keys := memoryKeys(f.search(f.query(f.root, 2))); len(keys) != 2 || !containsKey(keys, "current") {
		t.Fatalf("published state absent: %v", keys)
	}
	// Character memory never gains timeline narrative or another owner's data.
	character, _ := id.New()
	other, _ := id.New()
	for _, owner := range []id.ID{character, other} {
		doc := f.document(f.root, owner.String(), "branch_state", "дракон", 2, nil, v)
		f.exec(`UPDATE memory_embeddings SET owner_type='character',owner_id=$2 WHERE id=$1`, doc, owner)
	}
	q := f.query(f.root, 2)
	q.OwnerType = memory.OwnerCharacter
	q.OwnerID = character
	if rows := f.search(q); len(rows) != 1 || rows[0].OwnerID != character {
		t.Fatalf("character scope: %+v", rows)
	}
}

func containsKey(keys []string, key string) bool {
	for _, value := range keys {
		if value == key {
			return true
		}
	}
	return false
}

func TestHybridSearchRussianRRFAndDeterministicDeduplication(t *testing.T) {
	f := newHybridFixture(t)
	f.document(f.root, "vector-only", "historical_beat", "без совпадений", 1, nil, hybridVector(1, 0))
	f.document(f.root, "both", "historical_beat", "дракон дракон дракон", 2, nil, hybridVector(.8, .6))
	f.document(f.root, "lexical", "historical_beat", "дракон", 1, nil, hybridVector(0, 1))
	q := f.query(f.root, 2)
	rows := f.search(q)
	if !reflect.DeepEqual(memoryKeys(rows), []string{"both", "lexical", "vector-only"}) {
		t.Fatalf("RRF order: %+v", rows)
	}
	for index, want := range []float64{1.0/62 + 1.0/61, 1.0/63 + 1.0/62, 1.0 / 61} {
		if math.Abs(rows[index].Score-want) > 1e-10 {
			t.Fatalf("score[%d]=%v want %v", index, rows[index].Score, want)
		}
	}
	// Two rankings contain the same document, but the result contains it once.
	q.Limit = 1
	if got := memoryKeys(f.search(q)); !reflect.DeepEqual(got, []string{"both"}) {
		t.Fatal(got)
	}
	q.Text = ""
	q.Limit = 3
	if got := memoryKeys(f.search(q)); !reflect.DeepEqual(got, []string{"vector-only", "both", "lexical"}) {
		t.Fatal(got)
	}
	q.Text = "и на в" // Russian stop words must safely reduce to vector-only.
	if got := memoryKeys(f.search(q)); !reflect.DeepEqual(got, []string{"vector-only", "both", "lexical"}) {
		t.Fatal(got)
	}
	q.Text = "драконом"
	for range 3 {
		if got := f.search(q); !reflect.DeepEqual(got, rows) {
			t.Fatal("unstable ranking")
		}
	}
	// Duplicate overlapping source versions are collapsed before either rank.
	until := int64(2)
	f.document(f.root, "both", "historical_beat", "дракон", 1, &until, hybridVector(1, 0))
	if got := f.search(q); !reflect.DeepEqual(got, rows) {
		t.Fatalf("source duplication changed ranks: %+v", got)
	}

	capture := &capturingMemoryQuerier{tx: f.tx}
	if _, err := pg.NewHybridMemoryRetriever(capture).Search(f.ctx, q); err != nil {
		t.Fatal(err)
	}
	planRows, err := f.tx.Query(f.ctx, "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+capture.sql, capture.args...)
	if err != nil {
		t.Fatal(err)
	}
	defer planRows.Close()
	if !planRows.Next() {
		t.Fatal("missing plan")
	}
	var plan string
	if err = planRows.Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plan, "memory_embeddings_vector_hnsw") || !strings.Contains(plan, "CTE Scan") {
		t.Fatalf("unexpected exact-search plan: %s", plan)
	}
	t.Log("EXPLAIN ANALYZE verified materialized exact candidate ranking without HNSW (small synthetic fixture)")
}

func TestHybridSearchBreaksEqualRanksByOriginalID(t *testing.T) {
	f := newHybridFixture(t)
	first := f.document(f.root, "one", "historical_beat", "дракон", 1, nil, hybridVector(1, 0))
	second := f.document(f.root, "two", "historical_beat", "дракон", 1, nil, hybridVector(1, 0))
	want := []id.ID{first, second}
	if first > second {
		want[0], want[1] = second, first
	}
	for range 3 {
		rows := f.search(f.query(f.root, 2))
		if len(rows) != 2 || rows[0].ID != want[0] || rows[1].ID != want[1] {
			t.Fatalf("unstable equal-distance ranking: %+v", rows)
		}
	}
}

func TestHybridSearchFiltersKindsBeforeEitherRankWindow(t *testing.T) {
	f := newHybridFixture(t)
	allowed := f.document(f.root, "allowed", "historical_beat", "без совпадений", 1, nil, hybridVector(0, 1))
	f.exec(`UPDATE memory_embeddings SET kind='beat' WHERE id=$1`, allowed)
	for i := range 12 {
		blocked := f.document(f.root, fmt.Sprint("rule-", i), "historical_beat", "дракон дракон дракон", 1, nil, hybridVector(1, 0))
		f.exec(`UPDATE memory_embeddings SET kind='world_rule' WHERE id=$1`, blocked)
	}
	q := f.query(f.root, 2)
	q.Limit = 1
	q.Kinds = []string{"beat"}
	if rows := f.search(q); len(rows) != 1 || rows[0].ID != allowed {
		t.Fatalf("forbidden kinds consumed rank window: %+v", rows)
	}
	q.Kinds = []string{"missing"}
	if rows := f.search(q); len(rows) != 0 {
		t.Fatal("unknown kind returned data")
	}
}

type capturingMemoryQuerier struct {
	tx   pgx.Tx
	sql  string
	args []any
}

func (q *capturingMemoryQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	q.sql, q.args = sql, args
	return q.tx.Query(ctx, sql, args...)
}
