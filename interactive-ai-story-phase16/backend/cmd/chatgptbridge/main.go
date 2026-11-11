// chatgptbridge provides opt-in local browser transport for project and diagnostic requests.
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	storyllm "github.com/local/interactive-ai-story/backend/internal/adapters/storyllm/openai_compatible"
)

//go:embed web/*
var assets embed.FS

type assistantDiagnostic struct {
	AssistantCount int  `json:"assistantCount"`
	ScopedRoots    int  `json:"scopedRoots"`
	GlobalRoots    int  `json:"globalRoots"`
	TextLength     int  `json:"textLength"`
	RawTextLength  int  `json:"rawTextLength"`
	CodeBlocks     int  `json:"codeBlocks"`
	StopVisible    bool `json:"stopVisible"`
	CopyVisible    bool `json:"copyVisible"`
}
type workerDiagnostic struct {
	Assistant      *assistantDiagnostic `json:"assistant,omitempty"`
	Version        string               `json:"version"`
	Stage          string               `json:"stage"`
	Message        string               `json:"message"`
	ExpectedLength *int                 `json:"expectedLength,omitempty"`
	ActualLength   *int                 `json:"actualLength,omitempty"`
}
type job struct {
	Background         bool              `json:"background,omitempty"`
	Project            bool              `json:"project,omitempty"`
	ReasoningEffort    string            `json:"reasoningEffort,omitempty"`
	ResponseTimeoutMS  int               `json:"responseTimeoutMs,omitempty"`
	InputEncoding      string            `json:"inputEncoding,omitempty"`
	MessageFormat      string            `json:"messageFormat,omitempty"`
	MessageMarker      string            `json:"messageMarker,omitempty"`
	MessageContent     string            `json:"messageContent,omitempty"`
	RecoverSafeFailure bool              `json:"recoverSafeFailure,omitempty"`
	WorkerVersion      string            `json:"workerVersion,omitempty"`
	Diagnostic         *workerDiagnostic `json:"diagnostic,omitempty"`
	Kind               string            `json:"kind,omitempty"`
	ID                 string            `json:"id"`
	Prompt             string            `json:"prompt"`
	State              string            `json:"state"`
	Worker             string            `json:"worker,omitempty"`
	Lease              string            `json:"lease,omitempty"`
	Created            time.Time         `json:"created"`
	Deadline           time.Time         `json:"deadline"`
	Sent               *time.Time        `json:"sent,omitempty"`
	Finished           *time.Time        `json:"finished,omitempty"`
	Reply              string            `json:"reply,omitempty"`
	Error              string            `json:"error,omitempty"`
}
type workerState struct {
	Version string    `json:"version,omitempty"`
	Seen    time.Time `json:"seen"`
	Status  string    `json:"status"`
}
type bridge struct {
	mu          sync.Mutex
	token       string
	host        string
	jobs        []*job
	workers     map[string]workerState
	leaseWarmup time.Duration
	leaseReady  map[string]time.Time
	maxActive   int
	now         func() time.Time
}

func secret() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func newBridge(host string) *bridge {
	return &bridge{token: secret(), host: host, workers: map[string]workerState{}, leaseReady: map[string]time.Time{}, maxActive: storyllm.DefaultBrowserParallelism, now: time.Now}
}
func active(s string) bool { return s == "queued" || s == "leased" || s == "dispatched" }
func (b *bridge) expire() {
	now := b.now()
	for _, j := range b.jobs {
		if active(j.State) && !now.Before(j.Deadline) {
			if j.State == "dispatched" {
				j.State = "uncertain"
			} else {
				j.State = "failed"
			}
			j.Error = "deadline exceeded; no automatic retry"
			j.Finished = &now
		}
	}
	for id, t := range b.workers {
		if now.Sub(t.Seen) > 5*time.Minute {
			delete(b.workers, id)
			delete(b.leaseReady, id)
		}
	}
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	limit := int64(64 << 10)
	if r.URL.Path == "/result" || (r.URL.Path == "/v1/chat/completions" && r.Header.Get("X-Story-Project") == "1") {
		limit = 512 << 10
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		http.Error(w, "invalid body", 400)
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		http.Error(w, "trailing body", 400)
		return false
	}
	return true
}
func validReply(reply, id string) bool {
	s := strings.TrimSpace(reply)
	if strings.HasPrefix(s, "```json\n") && strings.HasSuffix(s, "\n```") {
		s = strings.TrimSuffix(strings.TrimPrefix(s, "```json\n"), "\n```")
	}
	var value struct {
		ID   string `json:"probe_id"`
		OK   bool   `json:"ok"`
		Text string `json:"text"`
	}
	d := json.NewDecoder(strings.NewReader(s))
	d.DisallowUnknownFields()
	return d.Decode(&value) == nil && d.Decode(new(any)) == io.EOF && value.ID == id && value.OK && value.Text == "Фонарь осветил пустую площадь."
}
func (b *bridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	if r.Host != b.host {
		http.Error(w, "invalid host", 403)
		return
	}
	origin := r.Header.Get("Origin")
	if origin != "" && origin != "http://"+b.host && origin != "https://chatgpt.com" {
		http.Error(w, "invalid origin", 403)
		return
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "cross-site request", 403)
		return
	}
	if r.Method == "GET" {
		switch r.URL.Path {
		case "/":
			data, _ := assets.ReadFile("web/index.html")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(bytes.ReplaceAll(data, []byte("BRIDGE_TOKEN"), []byte(b.token)))
			return
		case "/dashboard.js", "/style.css", "/worker.user.js":
			name := strings.TrimPrefix(r.URL.Path, "/")
			data, _ := assets.ReadFile("web/" + name)
			if strings.HasSuffix(name, ".css") {
				w.Header().Set("Content-Type", "text/css")
			} else {
				w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			}
			_, _ = w.Write(data)
			return
		}
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+b.token)) != 1 {
		http.Error(w, "unauthorized", 401)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/v1/chat/completions" {
		b.chatCompletions(w, r)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.expire()
	if r.Method == "GET" && r.URL.Path == "/status" {
		// Lease credentials are never exposed in the dashboard or probe report.
		out := make([]job, 0, len(b.jobs))
		for _, j := range b.jobs {
			v := *j
			v.Lease = ""
			out = append(out, v)
		}
		writeJSON(w, map[string]any{"jobs": out, "workers": b.workers, "max_active": b.maxActive, "lease_warmup_ms": b.leaseWarmup.Milliseconds()})
		return
	}
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	if r.URL.Path == "/probes" {
		var in struct {
			Count int `json:"count"`
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Count != 1 && in.Count != 3 {
			http.Error(w, "count must be 1 or 3", 400)
			return
		}
		for _, j := range b.jobs {
			if active(j.State) {
				http.Error(w, "a probe is still active", 409)
				return
			}
		}
		if len(b.jobs) > 57 {
			b.jobs = b.jobs[len(b.jobs)-57:]
		}
		for i := 0; i < in.Count; i++ {
			id := secret()
			expected := fmt.Sprintf(`{"probe_id":%q,"ok":true,"text":"Фонарь осветил пустую площадь."}`, id)
			b.jobs = append(b.jobs, &job{ID: id, Prompt: "Synthetic bridge probe. Return ONLY this JSON object, no markdown. Copy every value exactly: " + expected, State: "queued", Created: b.now(), Deadline: b.now().Add(3 * time.Minute)})
		}
		writeJSON(w, map[string]any{"queued": in.Count})
		return
	}
	var in struct {
		Version string `json:"version,omitempty"`
		Worker  string `json:"worker"`
		ID      string `json:"id"`
		Lease   string `json:"lease"`
		Reply   string `json:"reply"`
		Error   string `json:"error"`
		Status  string `json:"status"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Worker) < 8 || len(in.Worker) > 80 {
		http.Error(w, "invalid worker", 400)
		return
	}
	if _, ok := b.workers[in.Worker]; !ok && len(b.workers) >= 4096 {
		http.Error(w, "worker limit", 429)
		return
	}
	status := "ready"
	if in.Status != "" {
		switch in.Status {
		case "ready", "busy", "attention", "stopped":
			status = in.Status
		default:
			http.Error(w, "invalid worker status", 400)
			return
		}
	}
	version := in.Version
	if version == "" {
		version = b.workers[in.Worker].Version
	}
	if len(version) > 20 {
		http.Error(w, "invalid worker version", 400)
		return
	}
	b.workers[in.Worker] = workerState{Seen: b.now(), Status: status, Version: version}
	if r.URL.Path == "/heartbeat" {
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	if r.URL.Path == "/claim" {
		for _, j := range b.jobs {
			if active(j.State) && j.Worker == in.Worker {
				http.Error(w, "worker already has a job", 409)
				return
			}
		}
		n := 0
		for _, j := range b.jobs {
			if j.State == "leased" || j.State == "dispatched" {
				n++
			}
		}
		if n >= b.maxActive {
			w.WriteHeader(204)
			return
		}
		if b.leaseWarmup > 0 {
			ready, ok := b.leaseReady[in.Worker]
			if !ok {
				b.leaseReady[in.Worker] = b.now().Add(b.leaseWarmup)
				w.WriteHeader(204)
				return
			}
			if b.now().Before(ready) {
				w.WriteHeader(204)
				return
			}
		}
		// Prefer foreground work without preempting an already leased job.
		ordered := make([]*job, 0, len(b.jobs))
		for _, background := range []bool{false, true} {
			for _, j := range b.jobs {
				if j.Background == background {
					ordered = append(ordered, j)
				}
			}
		}
		for _, j := range ordered {
			if j.State == "queued" {
				if (j.Project || (j.ReasoningEffort != "" && j.ReasoningEffort != "none")) && version != "0.3.0" {
					continue
				}
				if j.MessageFormat == "readable-json-v3" && version != "0.2.2" && version != "0.3.0" {
					continue
				}
				if j.Project || (j.ReasoningEffort != "" && j.ReasoningEffort != "none") {
					j.Deadline = b.now().Add(storyllm.BrowserTimeout(j.ReasoningEffort))
				}
				j.State = "leased"
				j.Worker = in.Worker
				j.WorkerVersion = version
				j.Lease = secret()
				writeJSON(w, j)
				return
			}
		}
		w.WriteHeader(204)
		return
	}
	var j *job
	for _, v := range b.jobs {
		if v.ID == in.ID {
			j = v
			break
		}
	}
	if j == nil || j.Worker != in.Worker || in.Lease == "" || subtle.ConstantTimeCompare([]byte(j.Lease), []byte(in.Lease)) != 1 {
		http.Error(w, "invalid lease", 409)
		return
	}
	switch r.URL.Path {
	case "/send":
		if j.State != "leased" {
			http.Error(w, "send already authorized or expired", 409)
			return
		}
		now := b.now()
		j.State = "dispatched"
		j.Sent = &now
		writeJSON(w, map[string]bool{"authorized": true})
	case "/result":
		if j.State == "complete" && j.Reply == in.Reply {
			writeJSON(w, map[string]any{"accepted": true, "state": "complete"})
			return
		}
		if j.State != "dispatched" {
			http.Error(w, "job is not dispatched", 409)
			return
		}
		now := b.now()
		j.Finished = &now
		delete(b.leaseReady, j.Worker)
		valid := validReply(in.Reply, j.ID)
		if j.Kind == "chat" {
			_, valid = jsonReply(in.Reply)
			j.Reply = in.Reply
		}
		if len(in.Error) <= 2048 {
			var diagnostic workerDiagnostic
			if json.Unmarshal([]byte(in.Error), &diagnostic) == nil && len(diagnostic.Version) <= 20 && len(diagnostic.Stage) <= 100 && len(diagnostic.Message) <= 300 {
				j.Diagnostic = &diagnostic
			}
		}
		if !valid {
			j.State = "failed"
			j.Error = "invalid JSON, probe ID or content"
		} else {
			j.State = "complete"
			j.Reply = in.Reply
		}
		writeJSON(w, map[string]any{"state": j.State})
	case "/fail":
		if !active(j.State) {
			http.Error(w, "job is terminal", 409)
			return
		}
		if j.State == "dispatched" {
			j.State = "uncertain"
		} else {
			j.State = "failed"
		}
		now := b.now()
		j.Finished = &now
		// No browser text, account details or prompt contents in error logs.
		delete(b.leaseReady, j.Worker)
		j.Error = "worker stopped; inspect worker tab"
		if len(in.Error) <= 2048 {
			var diagnostic workerDiagnostic
			if json.Unmarshal([]byte(in.Error), &diagnostic) == nil && len(diagnostic.Version) <= 20 && len(diagnostic.Stage) <= 100 && len(diagnostic.Message) <= 300 {
				j.Diagnostic = &diagnostic
			}
		}
		writeJSON(w, map[string]string{"state": j.State})
	default:
		http.NotFound(w, r)
	}
}
func main() {
	host := "127.0.0.1:8791"
	b := newBridge(host)
	b.leaseWarmup = 2 * time.Second
	if raw := os.Getenv("CHATGPT_BRIDGE_LEASE_WARMUP_MS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 10000 {
			log.Fatal("CHATGPT_BRIDGE_LEASE_WARMUP_MS must be 0..10000")
		}
		b.leaseWarmup = time.Duration(n) * time.Millisecond
	}
	if path := os.Getenv("CHATGPT_BRIDGE_TOKEN_FILE"); path != "" {
		token, err := tokenFile(path, b.token)
		if err != nil {
			log.Fatal(err)
		}
		b.token = token
	}
	if raw := os.Getenv("CHATGPT_BRIDGE_MAX_ACTIVE"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 4096 {
			log.Fatal("CHATGPT_BRIDGE_MAX_ACTIVE must be 1..4096")
		}
		b.maxActive = n
	}
	srv := &http.Server{Addr: host, Handler: b, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 25 * time.Minute, IdleTimeout: 30 * time.Second}
	log.Printf("Local ChatGPT bridge: http://%s", host)
	if err := srv.ListenAndServe(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
