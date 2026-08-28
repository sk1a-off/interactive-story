package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	localimages "github.com/local/interactive-ai-story/backend/internal/adapters/imagestorage/local"
	pgadapter "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	secretfile "github.com/local/interactive-ai-story/backend/internal/adapters/secrets/file"
	storyllm "github.com/local/interactive-ai-story/backend/internal/adapters/storyllm/openai_compatible"
	storyrouter "github.com/local/interactive-ai-story/backend/internal/adapters/storyllm/role_router"
	appaiconfig "github.com/local/interactive-ai-story/backend/internal/application/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/application/canon"
	appdir "github.com/local/interactive-ai-story/backend/internal/application/director"
	appgen "github.com/local/interactive-ai-story/backend/internal/application/generation"
	appimagegen "github.com/local/interactive-ai-story/backend/internal/application/imagegeneration"
	appjobs "github.com/local/interactive-ai-story/backend/internal/application/jobs"
	appnarration "github.com/local/interactive-ai-story/backend/internal/application/narration"
	appprompt "github.com/local/interactive-ai-story/backend/internal/application/promptset"
	"github.com/local/interactive-ai-story/backend/internal/application/saves"
	"github.com/local/interactive-ai-story/backend/internal/application/storysetup"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	"github.com/local/interactive-ai-story/backend/internal/config"
	domainconfig "github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domainprompt "github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	"github.com/local/interactive-ai-story/backend/internal/domain/savepoint"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/observability"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/transport/httpapi"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	logger := observability.NewLogger(cfg.AppEnv)

	handler := httpapi.NewRouter()
	var closeDatabase func()
	var startWorker func(context.Context)
	if cfg.DatabaseURL != "" {
		startupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		pool, openErr := bootstrap.OpenPostgres(startupCtx, cfg.DatabaseURL)
		cancel()
		if openErr != nil {
			logger.Error("postgres unavailable", "error", openErr)
			os.Exit(1)
		}
		closeDatabase = pool.Close
		saveService := saves.NewService(pgadapter.NewUnitOfWork(pool), time.Now, nil)
		ownerID, parseErr := id.Parse(cfg.LocalOwnerID)
		if parseErr != nil {
			logger.Error("invalid local owner id", "error", parseErr)
			os.Exit(1)
		}
		bootLLM, llmErr := storyllm.New(storyllm.Config{BaseURL: cfg.StoryLLMBaseURL, APIKey: cfg.StoryLLMAPIKey, Model: cfg.StoryLLMModel, Profile: cfg.StoryLLMProfile, Timeout: 120 * time.Second}, nil)
		if llmErr != nil {
			logger.Error("story llm config invalid", "error", llmErr)
			os.Exit(1)
		}
		startupCtx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
		if err := bootstrap.EnsureLocalUser(startupCtx2, pool, ownerID, cfg.LocalUsername); err != nil {
			cancel2()
			logger.Error("local user bootstrap failed", "error", err)
			os.Exit(1)
		}
		if err := bootstrap.EnsureAIConfig(startupCtx2, pool, bootLLM.Identity()); err != nil {
			cancel2()
			logger.Error("ai config bootstrap failed", "error", err)
			os.Exit(1)
		}
		if err := bootstrap.EnsurePromptSet(startupCtx2, pool); err != nil {
			cancel2()
			logger.Error("prompt set bootstrap failed", "error", err)
			os.Exit(1)
		}
		cancel2()
		configRepo := pgadapter.NewAIConfigRepository(pool)
		promptRepo := pgadapter.NewPromptSetRepository(pool)
		secretStore, secretErr := secretfile.New(cfg.SecretStorePath)
		if secretErr != nil {
			logger.Error("provider secret store unavailable", "error", secretErr)
			os.Exit(1)
		}
		activeAtBoot, activeErr := configRepo.Active(context.Background())
		if activeErr != nil {
			logger.Error("active ai config unavailable", "error", activeErr)
			os.Exit(1)
		}
		if cfg.StoryLLMAPIKey != "" {
			_ = secretStore.Set(context.Background(), appaiconfig.SecretKey(activeAtBoot.ID), cfg.StoryLLMAPIKey)
		}
		aiSettingsService := appaiconfig.Service{Repo: configRepo, Secrets: secretStore}
		promptSettingsService := appprompt.Service{Repo: promptRepo}
		storyClient := func(ctx context.Context, rev domainconfig.Revision, promptRev *domainprompt.Revision) (aiport.StoryLLM, error) {
			var public domainconfig.PublicSettings
			_ = json.Unmarshal(rev.Settings, &public)
			endpoint := public.StoryLLMEndpoint
			if endpoint == "" {
				endpoint = cfg.StoryLLMBaseURL
			}
			keys := appaiconfig.CredentialKeys(ctx, secretStore, rev.ID)
			if rev.StoryLLM.Provider == domainconfig.ProviderGoogleGemini && len(keys) == 0 {
				return nil, errors.New("Google AI API key is not configured for the active AI revision")
			}
			if public.StoryLLMSafeHybrid {
				fast, err := storyllm.New(storyllm.Config{BaseURL: endpoint, APIKeys: keys, Provider: domainconfig.ProviderGoogleGemini, Model: domainconfig.Gemini35FlashLiteModel, Profile: "safe-hybrid-fast", Timeout: 120 * time.Second, PromptSet: promptRev}, nil)
				if err != nil {
					return nil, err
				}
				quality, err := storyllm.New(storyllm.Config{BaseURL: endpoint, APIKeys: keys, Provider: domainconfig.ProviderGoogleGemini, Model: domainconfig.AntigravityModel, Profile: "safe-hybrid-quality", Timeout: 5 * time.Minute, PromptSet: promptRev}, nil)
				if err != nil {
					return nil, err
				}
				return storyrouter.NewSafeHybrid(fast, quality)
			}
			return storyllm.New(storyllm.Config{BaseURL: endpoint, APIKeys: keys, Provider: rev.StoryLLM.Provider, Model: rev.StoryLLM.Model, Profile: rev.StoryLLM.Profile, Timeout: 120 * time.Second, PromptSet: promptRev}, nil)
		}
		storyFactory := func(ctx context.Context) (aiport.StoryLLM, error) {
			rev, err := configRepo.Active(ctx)
			if err != nil {
				return nil, err
			}
			promptRev, err := promptRepo.Active(ctx)
			if err != nil {
				return nil, err
			}
			return storyClient(ctx, rev, &promptRev)
		}
		imageStoryFactory := func(ctx context.Context) (aiport.StoryLLM, id.ID, error) {
			rev, err := configRepo.Active(ctx)
			if err != nil {
				return nil, "", err
			}
			promptRev, err := promptRepo.Active(ctx)
			if err != nil {
				return nil, "", err
			}
			llm, err := storyClient(ctx, rev, &promptRev)
			return llm, promptRev.ID, err
		}
		setupRepo := pgadapter.NewSetupRepository(pool)
		setupService := storysetup.Service{Repo: setupRepo, LLMFactory: storyFactory, Now: time.Now, OwnerID: story.UserID(ownerID)}
		readerRepo := pgadapter.NewReaderRepository(pool)
		directorRepo := pgadapter.NewDirectorRepository(pool)
		directorService := appdir.Service{Repo: directorRepo, LLMFactory: appdir.LLMFactory(storyFactory), Safety: func(ctx context.Context, tid timeline.ID, note string) (id.ID, error) {
			v, err := saveService.Create(ctx, saves.CreateCommand{TimelineID: tid, Name: "Director safety", Note: note, Kind: savepoint.KindSystem, Pinned: false})
			if err != nil {
				return "", err
			}
			return id.ID(v.ID), nil
		}}
		canonService := canon.NewService(pgadapter.NewUnitOfWork(pool), time.Now)
		hub := appgen.NewHub()
		jobQueue := pgadapter.NewJobQueue(pool)
		metadata := pgadapter.NewGenerationMetadata(pool)
		generationTarget := pgadapter.NewGenerationTarget(pool)
		updateStore := pgadapter.NewGenerationUpdates(pool)
		gameRunner := &appgen.DurableRunner{Queue: jobQueue, Config: configRepo, Metadata: metadata, PromptSets: promptRepo, Hub: hub, Updates: updateStore, MaxAttempts: 3}
		resolvePipeline := func(ctx context.Context, rev domainconfig.Revision, promptRev *domainprompt.Revision) (appgen.Pipeline, error) {
			if promptRev == nil {
				activePrompt, promptErr := promptRepo.Active(ctx)
				if promptErr != nil {
					return appgen.Pipeline{}, promptErr
				}
				promptRev = &activePrompt
			}
			llm, err := storyClient(ctx, rev, promptRev)
			if err != nil {
				return appgen.Pipeline{}, err
			}
			var public domainconfig.PublicSettings
			_ = json.Unmarshal(rev.Settings, &public)
			return appgen.Pipeline{LLM: llm, MaxRepairs: 1, Canon: canonService, Instructions: directorRepo, Targets: generationTarget, RuleAudits: generationTarget, WorldRulesSafeMode: public.WorldRulesSafeMode}, nil
		}
		executor := appgen.DurableExecutor{Config: configRepo, Prompts: promptRepo, Resolve: resolvePipeline, Hub: hub, Sink: appgen.PersistedSink{Store: updateStore, Live: hub}}
		imageStorage, storageErr := localimages.New(cfg.MediaRoot, cfg.MediaBaseURL)
		if storageErr != nil {
			logger.Error("image storage unavailable", "error", storageErr)
			os.Exit(1)
		}
		imageGenerationRepo := pgadapter.NewImageGenerationRepository(pool)
		imageService := appimagegen.Service{Repo: imageGenerationRepo, Configs: configRepo, Prompts: appimagegen.LLMPromptBuilder{LLMFactory: appimagegen.StoryLLMFactory(imageStoryFactory)}, Storage: imageStorage, Now: time.Now, LeaseTimeout: 5 * time.Minute, Logger: logger}
		narrationService, narrationErr := appnarration.NewService(cfg.OmniVoiceURL, nil)
		if narrationErr != nil {
			logger.Error("OmniVoice narration config invalid", "error", narrationErr)
			os.Exit(1)
		}
		workers := make([]appjobs.Worker, 3)
		for index := range workers {
			workers[index] = appjobs.Worker{Queue: jobQueue, Executor: executor, Owner: fmt.Sprintf("backend-worker-%d", index+1), LeaseTTL: 30 * time.Second, HeartbeatEvery: 10 * time.Second, ExecutionTimeout: 3 * time.Minute, Backoff: appjobs.DefaultBackoff, Now: time.Now}
		}
		promptWorker := appimagegen.PromptJobWorker{Repo: imageGenerationRepo, Images: imageService, Owner: "image-prompt-worker-1", LocalOwnerID: ownerID, LeaseTTL: 3 * time.Minute, ExecutionTimeout: 2 * time.Minute, Now: time.Now, Logger: logger}
		startWorker = func(ctx context.Context) {
			logger.Info("generation workers starting", "story_workers", len(workers), "local_provider_parallelism", 1, "image_prompt_workers", 1)
			startRestarting := func(name string, run func(context.Context) error) {
				go func() {
					for ctx.Err() == nil {
						err := run(ctx)
						if err == nil || errors.Is(err, context.Canceled) || ctx.Err() != nil {
							return
						}
						logger.Error(name+" cycle failed; restarting", "error", err)
						select {
						case <-ctx.Done():
							return
						case <-time.After(time.Second):
						}
					}
				}()
			}
			for index := range workers {
				worker := workers[index]
				startRestarting(worker.Owner, func(runCtx context.Context) error { return worker.Run(runCtx, 250*time.Millisecond) })
			}
			startRestarting(promptWorker.Owner, func(runCtx context.Context) error { return promptWorker.Run(runCtx, 250*time.Millisecond) })
		}
		handler = httpapi.NewRouterWithApplications(saveService, gameRunner, setupService, setupRepo, readerRepo, directorService, aiSettingsService, promptSettingsService, imageService, narrationService)
	}
	if closeDatabase != nil {
		defer closeDatabase()
	}

	serveHandler := handler
	if cfg.MediaRoot != "" {
		mux := http.NewServeMux()
		mediaPrefix := strings.TrimRight(cfg.MediaBaseURL, "/") + "/"
		mux.Handle(mediaPrefix, http.StripPrefix(mediaPrefix, http.FileServer(http.Dir(cfg.MediaRoot))))
		mux.Handle("/", handler)
		serveHandler = mux
	}
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           serveHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if startWorker != nil {
		startWorker(ctx)
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server starting", "addr", cfg.HTTPAddr, "app_env", cfg.AppEnv)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown requested")
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("http server stopped")
}
