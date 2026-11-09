package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	appgen "github.com/local/interactive-ai-story/backend/internal/application/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

type turnFixture struct {
	ID, Action, FakeText string
	Target               generationtarget.Target
	Memory               []string
}
type preparedTurnFixture struct {
	Fixture    turnFixture
	StoryID    id.ID
	TimelineID timeline.ID
}

func (f preparedTurnFixture) CurrentWriteTarget(context.Context, timeline.ID) (generationtarget.Target, error) {
	return f.Fixture.Target, nil
}
func turnFixtures() []turnFixture {
	target := generationtarget.Target{ChapterID: id.MustParse("00000000-0000-4000-8000-000000008001"), SceneID: id.MustParse("00000000-0000-4000-8000-000000008002"), ChapterNumber: 1, SceneNumber: 1, ChapterSceneCount: 1, ChapterBeatCount: 3, NextBeatPosition: 4, ChapterTitle: "Архив у реки", ChapterGoal: "Найти безопасный путь к северной башне", SceneGoal: "Разобраться с проходом через архив", SceneMood: "настороженное спокойствие",
		StoryBible:       json.RawMessage(`{"title":"Архив у реки","premise":"Путник Илья и архивистка Мира ищут путь к северной башне после паводка.","language":"Russian","tone":"grounded adventure"}`),
		Player:           json.RawMessage(`{"name":"Илья","description":"Внимательный путник в синем дорожном плаще","goal":"Дойти до северной башни"}`),
		World:            json.RawMessage(`{"locations":[{"id":"00000000-0000-4000-8000-000000008003","name":"Архивная пристань","description":"Каменный архив у затопленной площади. Внутренняя дверь заперта."}],"time":"вечер"}`),
		InitialCast:      json.RawMessage(`{"characters":[{"id":"00000000-0000-4000-8000-000000008004","name":"Илья","age":28,"role":"protagonist","personality":"осторожный","currentGoal":"найти путь к башне"},{"id":"00000000-0000-4000-8000-000000008005","name":"Мира","age":31,"role":"archivist","personality":"сдержанная","currentGoal":"сохранить архив от воды"}]}`),
		PreviousBeatText: "Мира остановилась перед запертой дверью архива. Илья развернул карту, прислушиваясь к шуму воды за стеной.",
		Journal:          []generationtarget.JournalEntry{{ID: "00000000-0000-4000-8000-000000008006", Category: "inventory", Name: "Карта водовода", Quantity: 1, Status: "active"}},
	}
	paragraphs := []string{"Илья положил ладонь на сухой край стола и перевёл взгляд на Миру.", "Архивистка отодвинула лампу, освободив место для развёрнутой карты.", "С дальнего края пристани донёсся плеск, и оба на мгновение замолчали.", "Мира указала на узкую линию возле северной стены: там начинался старый проход.", "Илья наклонился ближе и заметил рядом со знаком едва различимую пометку.", "За дверью что-то тихо скользнуло по камню, но никто не стал трогать засов.", "Мира придержала угол карты и выжидательно посмотрела на спутника, оставляя решение ему."}
	text := ""
	for i, p := range paragraphs {
		if i > 0 {
			text += "\n\n"
		}
		text += p
	}
	history := []string{"Раньше Мира обещала показать Илье проход через архив, если он сохранит найденную карту сухой.", "Внутренняя дверь архива закрыта засовом изнутри; запасной вход расположен со стороны водовода.", "Карта водовода получена до прихода на пристань. На ней отмечена лестница за помещением с медными трубами."}
	return []turnFixture{
		{ID: "conversation", Action: "Спокойно спросить Миру, где начинается обещанный проход, и выслушать её ответ.", Target: target, Memory: history, FakeText: text},
		{ID: "inspection", Action: "Осторожно осмотреть запертую дверь архива и её петли, не пытаясь вскрыть замок.", Target: target, Memory: history, FakeText: text},
		{ID: "danger", Action: "Отступить за каменную колонну и прислушаться к звуку за дверью, не атакуя вслепую.", Target: target, Memory: history, FakeText: text},
	}
}
func fakeTurnModel(f turnFixture) ai.StoryLLM {
	writer, _ := json.Marshal(map[string]string{"text": f.FakeText})
	planner, _ := json.Marshal(map[string]any{"intent": f.Action, "immediateGoal": "Уточнить путь и сохранить осторожность героя", "transition": "continue_scene", "scene": map[string]string{}, "chapter": map[string]string{}, "riskFlags": []string{}})
	model := fakeai.NewScriptedStoryLLM(map[string][]byte{"action_interpreter": []byte(`{"intent":"` + f.Action + `"}`), "turn_planner": planner, "director": []byte(`{"goal":"Уточнить путь и сохранить осторожность героя"}`), "pacing": []byte(`{"transition":"continue_scene"}`), "writer": writer, "world_evaluator": []byte(`{"changes":[]}`), "state_evaluator": []byte(`{"changes":[]}`), "quest_evaluator": []byte(`{"changes":[]}`), "choices": []byte(`{"choices":[{"id":"ask","label":"Расспросить Миру"},{"id":"map","label":"Изучить карту"},{"id":"listen","label":"Прислушаться к звукам"},{"id":"wait","label":"Подождать у колонны"}]}`)})
	// Exercise the same provider-gated parallel evaluator path mechanically.
	model.ID = ai.ProviderIdentity{Kind: ai.KindStoryLLM, Provider: "google_gemini", Model: "fake-scripted-pipeline", Profile: "smoke-only"}
	return model
}
func prepareTurnFixtures(ctx context.Context, pool *pgxpool.Pool, owner id.ID, fixtures []turnFixture, e ai.EmbeddingProvider) (out []preparedTurnFixture, err error) {
	var vectors [][][]float32
	for _, f := range fixtures {
		response, embedErr := e.EmbedDocuments(ctx, ai.EmbeddingRequest{Texts: f.Memory})
		if embedErr != nil {
			return nil, errors.New("memory documents unavailable")
		}
		if len(response.Vectors) != len(f.Memory) {
			return nil, errors.New("memory document shape invalid")
		}
		for _, v := range response.Vectors {
			if len(v) != 384 {
				return nil, errors.New("memory vector dimensions invalid")
			}
			norm := float64(0)
			for _, x := range v {
				if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
					return nil, errors.New("memory vector is not finite")
				}
				norm += float64(x) * float64(x)
			}
			if norm == 0 {
				return nil, errors.New("zero memory vector")
			}
		}
		vectors = append(vectors, response.Vectors)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO users(id,username) VALUES($1,$2)`, owner, "turnbench-"+owner.String()); err != nil {
		return nil, err
	}
	for i, f := range fixtures {
		story, _ := id.New()
		tid, _ := id.New()
		if _, err = tx.Exec(ctx, `INSERT INTO stories(id,owner_id,title) VALUES($1,$2,'Synthetic full-turn benchmark')`, story, owner); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO timelines(id,story_id,name,head_event_seq) VALUES($1,$2,'fixture',12)`, tid, story); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO memory_index_state(timeline_id,story_id,observed_event_seq) VALUES($1,$2,12)`, tid, story); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO memory_index_publication_state(timeline_id,provider_name,model_name,embedding_revision,published_event_seq) VALUES($1,$2,$3,1,12)`, tid, e.Identity().Provider, e.Identity().Model); err != nil {
			return nil, err
		}
		for j, text := range f.Memory {
			if len(vectors[i][j]) != 384 {
				return nil, errors.New("memory vector dimensions invalid")
			}
			vector, _ := json.Marshal(vectors[i][j])
			mid, _ := id.New()
			if _, err = tx.Exec(ctx, `INSERT INTO memory_embeddings(id,story_id,timeline_id,owner_type,owner_id,kind,content,embedding,embedding_provider,embedding_model,embedding_revision,valid_from_event_seq,source_key,document_class)
 VALUES($1,$2,$3,'timeline',$3,'beat',$4,$5::vector,$6,$7,1,$8,$9,'historical_beat')`, mid, story, tid, text, string(vector), e.Identity().Provider, e.Identity().Model, 2+j*2, fmt.Sprint("history-", j)); err != nil {
				return nil, err
			}
		}
		out = append(out, preparedTurnFixture{Fixture: f, StoryID: story, TimelineID: timeline.ID(tid)})
	}
	return out, tx.Commit(ctx)
}
func cleanupTurnFixtures(ctx context.Context, pool *pgxpool.Pool, owner id.ID) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM timelines WHERE story_id IN (SELECT id FROM stories WHERE owner_id=$1 AND title='Synthetic full-turn benchmark')`, owner); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM stories WHERE owner_id=$1 AND title='Synthetic full-turn benchmark'`, owner); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM users WHERE id=$1 AND username=$2`, owner, "turnbench-"+owner.String()); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	var count int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id=$1`, owner).Scan(&count)
	if err != nil {
		return err
	}
	if count != 0 {
		return errors.New("fixture owner retained")
	}
	return nil
}

type turnCaptureCanon struct{ choices int }

func (c *turnCaptureCanon) AppendSemantic(_ context.Context, tid timeline.ID, head int64, pending []event.PendingEvent) ([]event.StoredEvent, error) {
	var out []event.StoredEvent
	for i, p := range pending {
		if err := p.Validate(); err != nil {
			return nil, err
		}
		eid, _ := id.New()
		out = append(out, event.StoredEvent{ID: event.ID(eid), TimelineID: tid, Seq: head + int64(i) + 1, Type: p.Type, SchemaVersion: p.SchemaVersion, Payload: append([]byte(nil), p.Payload...), GenerationID: p.GenerationID, CreatedAt: time.Now()})
		if p.Type == "choices_ready" {
			var v struct {
				Choices []any `json:"choices"`
			}
			_ = json.Unmarshal(p.Payload, &v)
			c.choices = len(v.Choices)
		}
	}
	return out, nil
}

type turnBenchmarkSink struct {
	started             time.Time
	provisional         bool
	draft, first, ready *float64
	text                string
}

func (s *turnBenchmarkSink) Publish(_ context.Context, u appgen.Update) {
	now := float64(time.Since(s.started)) / float64(time.Millisecond)
	if u.Phase == appgen.PhaseWriting && u.TextDelta != "" {
		s.text = u.TextDelta
		if s.draft == nil {
			v := now
			s.draft = &v
		}
	}
	if u.Phase == appgen.PhaseDraftReady && u.Provisional && s.provisional && s.first == nil {
		v := now
		s.first = &v
	}
	if u.Phase == appgen.PhaseCompleted {
		v := now
		s.ready = &v
		if s.first == nil {
			f := now
			s.first = &f
		}
	}
}
