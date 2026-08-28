package aiconfig

import (
	"context"
	"encoding/json"
	"errors"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"strings"
	"testing"
)

type repo struct {
	rows   []domain.Revision
	active id.ID
}

func (r *repo) CreateRevision(_ context.Context, c domain.CreateRevisionCommand) (domain.Revision, error) {
	v, _ := domain.NewRevision(id.MustParse("00000000-0000-4000-8000-00000000000"+string(rune('1'+len(r.rows)))), int64(len(r.rows)+1), c.StoryLLM, c.Embedding, c.Image)
	v.Settings = c.Public.JSON()
	r.rows = append(r.rows, v)
	return v, nil
}
func (r *repo) Get(_ context.Context, i id.ID) (domain.Revision, error) {
	for _, v := range r.rows {
		if v.ID == i {
			return v, nil
		}
	}
	return domain.Revision{}, domain.ErrInvalidConfig
}
func (r *repo) List(context.Context) ([]domain.Revision, error) {
	return append([]domain.Revision(nil), r.rows...), nil
}
func (r *repo) Active(ctx context.Context) (domain.Revision, error) { return r.Get(ctx, r.active) }
func (r *repo) Activate(_ context.Context, i id.ID) error           { r.active = i; return nil }

type secretStub struct{ values map[string]string }

func (s *secretStub) Set(_ context.Context, k string, v string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[k] = v
	return nil
}
func (s *secretStub) Has(_ context.Context, k string) bool { return s.values[k] != "" }
func (s *secretStub) Get(_ context.Context, k string) (string, bool) {
	v, ok := s.values[k]
	return v, ok
}
func command(model string) domain.CreateRevisionCommand {
	return domain.CreateRevisionCommand{
		StoryLLM:  ai.ProviderIdentity{Kind: ai.KindStoryLLM, Provider: "openai_compatible", Model: model, Profile: "q5"},
		Embedding: ai.ProviderIdentity{Kind: ai.KindEmbedding, Provider: "local_sentence_transformers", Model: "deepvk/USER2-small", Profile: "cpu"},
		Image:     ai.ProviderIdentity{Kind: ai.KindImage, Provider: "fake", Model: "image-v1"},
		Public:    domain.PublicSettings{StoryLLMEndpoint: "http://127.0.0.1:8081", StoryLLMContext: 16384, StoryLLMKVDType: "q8_0"},
	}
}
func TestSafeViewNeverReturnsSecret(t *testing.T) {
	rp := &repo{}
	sec := &secretStub{values: map[string]string{}}
	svc := Service{Repo: rp, Secrets: sec}
	key := "super-secret"
	v, e := svc.Create(context.Background(), UpdateRequest{Config: command("m1"), StoryLLMAPIKey: &key, Activate: true})
	if e != nil {
		t.Fatal(e)
	}
	if !v.StoryLLMSecretConfigured {
		t.Fatal("secret presence flag missing")
	}
	if sec.values[SecretKey(id.MustParse(v.ID))] != "super-secret" {
		t.Fatal("secret not stored")
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "super-secret") {
		t.Fatal("safe settings view leaked secret")
	}
}

func TestMultipleSecretsAreDeduplicatedCountedAndWriteOnly(t *testing.T) {
	rp := &repo{}
	sec := &secretStub{values: map[string]string{}}
	svc := Service{Repo: rp, Secrets: sec}
	gemini := command(domain.GeminiFlashModel)
	gemini.StoryLLM.Provider = domain.ProviderGoogleGemini
	gemini.StoryLLM.Profile = "gemini-free-tier"
	gemini.Public.StoryLLMEndpoint = domain.GeminiOpenAIEndpoint
	view, err := svc.Create(context.Background(), UpdateRequest{Config: gemini, StoryLLMAPIKeys: []string{" key-one ", "key-two", "key-one"}, Activate: true})
	if err != nil {
		t.Fatal(err)
	}
	if view.StoryLLMSecretCount != 2 || !view.StoryLLMSecretConfigured {
		t.Fatalf("unexpected safe credential metadata: %#v", view)
	}
	keys := CredentialKeys(context.Background(), sec, id.MustParse(view.ID))
	if strings.Join(keys, ",") != "key-one,key-two" {
		t.Fatalf("stored keys = %#v", keys)
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "key-one") || strings.Contains(string(raw), "key-two") {
		t.Fatal("safe view leaked a Gemini credential")
	}
}
func TestSwitchCreatesNewRevisionWithoutMutatingOld(t *testing.T) {
	rp := &repo{}
	svc := Service{Repo: rp, Secrets: &secretStub{values: map[string]string{}}}
	first, e := svc.Create(context.Background(), UpdateRequest{Config: command("m1"), Activate: true})
	if e != nil {
		t.Fatal(e)
	}
	second, e := svc.Create(context.Background(), UpdateRequest{Config: command("m2"), Activate: true})
	if e != nil {
		t.Fatal(e)
	}
	if first.ID == second.ID || first.Revision == second.Revision {
		t.Fatal("switch did not create immutable revision")
	}
	old, _ := rp.Get(context.Background(), id.MustParse(first.ID))
	if old.StoryLLM.Model != "m1" {
		t.Fatal("old revision mutated")
	}
	active, _ := svc.Active(context.Background())
	if active.StoryLLM.Model != "m2" {
		t.Fatal("new revision not active")
	}
}

func TestRevisionSecretsDoNotOverwriteOlderRevision(t *testing.T) {
	rp := &repo{}
	sec := &secretStub{values: map[string]string{}}
	svc := Service{Repo: rp, Secrets: sec}
	one := "key-one"
	first, e := svc.Create(context.Background(), UpdateRequest{Config: command("m1"), StoryLLMAPIKey: &one, Activate: true})
	if e != nil {
		t.Fatal(e)
	}
	two := "key-two"
	second, e := svc.Create(context.Background(), UpdateRequest{Config: command("m2"), StoryLLMAPIKey: &two, Activate: true})
	if e != nil {
		t.Fatal(e)
	}
	firstID := id.MustParse(first.ID)
	secondID := id.MustParse(second.ID)
	if sec.values[SecretKey(firstID)] != "key-one" || sec.values[SecretKey(secondID)] != "key-two" {
		t.Fatal("revision-scoped secrets overwritten")
	}
	if _, e = svc.Activate(context.Background(), firstID); e != nil {
		t.Fatal(e)
	}
	if sec.values[SecretKey(firstID)] != "key-one" {
		t.Fatal("reactivating old revision lost old credential association")
	}
}

func TestSecretIsNotInheritedWhenProviderChanges(t *testing.T) {
	rp := &repo{}
	sec := &secretStub{values: map[string]string{}}
	svc := Service{Repo: rp, Secrets: sec}
	localKey := "local-key"
	if _, err := svc.Create(context.Background(), UpdateRequest{Config: command("m1"), StoryLLMAPIKey: &localKey, Activate: true}); err != nil {
		t.Fatal(err)
	}
	gemini := command(domain.GeminiFlashModel)
	gemini.StoryLLM.Provider = domain.ProviderGoogleGemini
	gemini.StoryLLM.Profile = "gemini-free-tier"
	gemini.Public.StoryLLMEndpoint = domain.GeminiOpenAIEndpoint
	_, err := svc.Create(context.Background(), UpdateRequest{Config: gemini, Activate: true})
	if !errors.Is(err, ErrGeminiAPIKeyRequired) {
		t.Fatalf("expected Gemini key requirement, got %v", err)
	}
	if len(rp.rows) != 1 {
		t.Fatal("invalid Gemini revision must not be created")
	}
}

func TestGeminiRevisionCanInheritOnlyGeminiCredential(t *testing.T) {
	rp := &repo{}
	sec := &secretStub{values: map[string]string{}}
	svc := Service{Repo: rp, Secrets: sec}
	gemini := command(domain.GeminiFlashModel)
	gemini.StoryLLM.Provider = domain.ProviderGoogleGemini
	gemini.StoryLLM.Profile = "gemini-free-tier"
	gemini.Public.StoryLLMEndpoint = domain.GeminiOpenAIEndpoint
	key := " gemini-key "
	first, err := svc.Create(context.Background(), UpdateRequest{Config: gemini, StoryLLMAPIKey: &key, Activate: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Create(context.Background(), UpdateRequest{Config: gemini, Activate: true})
	if err != nil {
		t.Fatal(err)
	}
	if !second.StoryLLMSecretConfigured || sec.values[SecretKey(id.MustParse(first.ID))] != "gemini-key" || sec.values[SecretKey(id.MustParse(second.ID))] != "gemini-key" {
		t.Fatal("Gemini credential was not safely inherited")
	}
}
