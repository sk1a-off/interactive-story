package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type formatSpec struct {
	ID, CaseID, Format string
	Repeat             int
}

func runPairedFormatProbe(o pairedOptions) error {
	if o.Output == "" {
		return errors.New("format probe requires a fresh output directory")
	}
	root, e := filepath.Abs(o.Output)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(root), 0755); e != nil {
		return e
	}
	if e = os.Mkdir(root, 0755); e != nil {
		return errors.New("format probe output already exists")
	}
	parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, 15*time.Minute)
	defer cancel()
	m, e := loadPairedModels(ctx, o)
	if e != nil {
		return e
	}
	if e = waitPairedWorkers(ctx); e != nil {
		return e
	}
	cases := pairedCorpus()
	specs := []formatSpec{}
	formats := []string{"legacy-json", "readable-json-v3"}
	if o.PlainFormat {
		formats = []string{"readable-json-v3", "readable-plain-json"}
	}
	for _, id := range []string{"turn_planner_browser", "writer_detective", "writer_archive"} {
		if _, ok := pairedCaseByID(cases, id); !ok {
			return fmt.Errorf("missing probe case %s", id)
		}
		for rep := 1; rep <= 2; rep++ {
			for _, format := range formats {
				specs = append(specs, formatSpec{ID: fmt.Sprintf("%s-r%d-%s", id, rep, format), CaseID: id, Repeat: rep, Format: format})
			}
		}
	}
	rand.New(rand.NewSource(o.Seed)).Shuffle(len(specs), func(i, j int) { specs[i], specs[j] = specs[j], specs[i] })
	_ = savePaired(filepath.Join(root, "plan.json"), map[string]any{"version": "format-v1", "schedule": specs, "promptRevision": m.promptRevision, "promptSHA256": promptFingerprint(m.prompts), "seed": o.Seed, "notes": []string{"Twelve independent calls, no retries; same logical inputs and active system prompts", "Two repeated calls per case per format; small diagnostic sample, not a general reliability estimate", "Version 0.2.2 worker for both formats; stable UI-empty is recorded separately from timeout", "Three simultaneous workers; model sampling controls unavailable in web UI"}})
	savePairedBridge(ctx, root, "before")
	queue := make(chan formatSpec)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range queue {
				c, _ := pairedCaseByID(cases, s.CaseID)
				callCtx := context.WithValue(ctx, pairedFormatKey{}, s.Format)
				row := compareGenerate(callCtx, "chatgpt", m.models["chatgpt"], c)
				_ = savePaired(filepath.Join(root, "outcomes", s.ID+".json"), map[string]any{"spec": s, "outcome": row})
				fmt.Printf("format case=%s format=%s repeat=%d valid=%t elapsed_ms=%d error=%s\n", s.CaseID, s.Format, s.Repeat, row.OK, row.ElapsedMS, row.ErrorCode)
			}
		}()
	}
	for _, s := range specs {
		if ctx.Err() != nil {
			break
		}
		select {
		case queue <- s:
		case <-ctx.Done():
		}
	}
	close(queue)
	wg.Wait()
	savePairedBridge(context.Background(), root, "after")
	status := "complete"
	if ctx.Err() != nil {
		status = "interrupted"
	}
	_ = savePaired(filepath.Join(root, "state.json"), map[string]any{"status": status, "time": time.Now().UTC()})
	return ctx.Err()
}
