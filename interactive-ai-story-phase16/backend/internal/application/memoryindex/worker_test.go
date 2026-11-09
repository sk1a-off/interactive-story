package memoryindex

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/memory"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/memoryindexrepo"
)

type queueStub struct {
	job           repo.Job
	heartbeatErr  error
	retries       int
	completeCalls int
	detail        string
}

func (q *queueStub) Claim(context.Context, string, time.Time, time.Duration) (repo.Job, error) {
	return q.job, nil
}
func (q *queueStub) Heartbeat(context.Context, repo.Job, time.Time, time.Duration) error {
	return q.heartbeatErr
}
func (q *queueStub) Complete(context.Context, repo.Job, time.Time) error {
	q.completeCalls++
	return nil
}
func (q *queueStub) Retry(_ context.Context, _ repo.Job, _ time.Time, _ time.Duration, detail string) error {
	q.retries++
	q.detail = detail
	return nil
}

type storeStub struct {
	documents    []domain.Document
	unchanged    bool
	publishCalls int
	vectors      map[string][]float32
}

func (s *storeStub) Load(context.Context, repo.Job) ([]domain.Document, error) {
	return s.documents, nil
}
func (s *storeStub) Changed(context.Context, repo.Job, repo.Profile, []domain.Document) ([]domain.Document, error) {
	if s.unchanged {
		return nil, nil
	}
	return s.documents, nil
}
func (s *storeStub) Publish(_ context.Context, _ repo.Job, _ repo.Profile, _ []domain.Document, vectors map[string][]float32, _ time.Time) error {
	s.publishCalls++
	s.vectors = vectors
	return nil
}

type embeddingStub struct {
	*fakeai.EmbeddingProvider
	mode    string
	batches []int
}

func (e *embeddingStub) EmbedQueries(context.Context, ai.EmbeddingRequest) (ai.EmbeddingResponse, error) {
	panic("indexing must use document embeddings")
}
func (e *embeddingStub) EmbedDocuments(ctx context.Context, request ai.EmbeddingRequest) (ai.EmbeddingResponse, error) {
	e.batches = append(e.batches, len(request.Texts))
	if e.mode == "blocked" {
		<-ctx.Done()
		return ai.EmbeddingResponse{}, ctx.Err()
	}
	if e.mode == "error" {
		return ai.EmbeddingResponse{}, errors.New("private source text")
	}
	result, err := e.EmbeddingProvider.EmbedDocuments(ctx, request)
	switch e.mode {
	case "count":
		result.Vectors = nil
	case "dimension":
		result.Vectors[0] = []float32{1}
	case "nan":
		result.Vectors[0][0] = float32(math.NaN())
	case "zero":
		clear(result.Vectors[0])
	}
	return result, err
}

func workerFixture(count int) (Worker, *queueStub, *storeStub, *embeddingStub) {
	queue := &queueStub{job: repo.Job{Attempts: 1}}
	store := &storeStub{}
	for index := 0; index < count; index++ {
		store.documents = append(store.documents, domain.Document{Key: fmt.Sprint(index), Content: "memory text"})
	}
	embeddings := &embeddingStub{EmbeddingProvider: fakeai.NewEmbeddingProvider(384)}
	return Worker{Queue: queue, Store: store, Embeddings: embeddings, Profile: repo.Profile{Provider: embeddings.Identity().Provider, Model: embeddings.Identity().Model, Revision: 1}, Owner: "worker"}, queue, store, embeddings
}

func TestWorkerBatchesAndPublishesWithoutSeparateAcknowledgement(t *testing.T) {
	worker, queue, store, embeddings := workerFixture(65)
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(embeddings.batches) != 2 || embeddings.batches[0] != 64 || embeddings.batches[1] != 1 || len(store.vectors) != 65 || store.publishCalls != 1 || queue.completeCalls != 0 || queue.retries != 0 {
		t.Fatalf("batches=%v vectors=%d publications=%d standalone complete=%d", embeddings.batches, len(store.vectors), store.publishCalls, queue.completeCalls)
	}
}

func TestWorkerSkipsUnchangedDocumentEmbeddings(t *testing.T) {
	worker, _, store, embeddings := workerFixture(1)
	store.unchanged = true
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(embeddings.batches) != 0 || store.publishCalls != 1 {
		t.Fatal("unchanged memory unnecessarily embedded or checkpoint not published")
	}
}

func TestWorkerRejectsInvalidEmbeddingsWithoutPublication(t *testing.T) {
	for _, mode := range []string{"count", "dimension", "nan", "zero", "error"} {
		t.Run(mode, func(t *testing.T) {
			worker, queue, store, embeddings := workerFixture(1)
			embeddings.mode = mode
			if err := worker.RunOnce(context.Background()); err == nil {
				t.Fatal("invalid output accepted")
			}
			if store.publishCalls != 0 || queue.retries != 1 || queue.detail != "memory indexing failed" {
				t.Fatalf("published=%d retries=%d detail=%s", store.publishCalls, queue.retries, queue.detail)
			}
		})
	}
}

func TestWorkerCancelsEmbeddingWhenLeaseIsLost(t *testing.T) {
	worker, queue, store, embeddings := workerFixture(1)
	queue.heartbeatErr = repo.ErrLeaseLost
	embeddings.mode = "blocked"
	worker.HeartbeatEvery = time.Millisecond
	if err := worker.RunOnce(context.Background()); !errors.Is(err, repo.ErrLeaseLost) {
		t.Fatalf("error=%v", err)
	}
	if store.publishCalls != 0 || queue.retries != 0 {
		t.Fatal("lost lease published data or mutated the reclaimed job")
	}
}
