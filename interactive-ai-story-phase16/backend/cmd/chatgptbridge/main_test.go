package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func call(b *bridge, path string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "http://"+b.host+path, strings.NewReader(string(data)))
	r.Header.Set("Authorization", "Bearer "+b.token)
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	return w
}
func claim(t *testing.T, b *bridge, worker string) job {
	t.Helper()
	w := call(b, "/claim", map[string]string{"worker": worker, "version": "0.2.2"})
	if w.Code != 200 {
		t.Fatalf("claim %d %s", w.Code, w.Body)
	}
	var j job
	if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
		t.Fatal(err)
	}
	return j
}
func lease(j job) map[string]string {
	return map[string]string{"worker": j.Worker, "id": j.ID, "lease": j.Lease}
}
func TestAtMostOnceAndCorrelatedResult(t *testing.T) {
	b := newBridge("127.0.0.1:8791")
	if w := call(b, "/probes", map[string]int{"count": 1}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	j := claim(t, b, "worker-one")
	body := lease(j)
	if w := call(b, "/claim", map[string]string{"worker": j.Worker}); w.Code != 409 {
		t.Fatal("duplicate claim", w.Code)
	}
	wrong := lease(j)
	wrong["worker"] = "worker-two"
	if w := call(b, "/send", wrong); w.Code != 409 {
		t.Fatal("wrong worker", w.Code)
	}
	if w := call(b, "/send", body); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := call(b, "/send", body); w.Code != 409 {
		t.Fatal("repeat send", w.Code)
	}
	body["reply"] = fmt.Sprintf(`{"probe_id":%q,"ok":true,"text":"Фонарь осветил пустую площадь."}`, j.ID)
	if w := call(b, "/result", body); w.Code != 200 || b.jobs[0].State != "complete" {
		t.Fatal(w.Code, b.jobs[0].State)
	}
	if w := call(b, "/result", body); w.Code != 200 {
		t.Fatal("idempotent upload", w.Code)
	}
	if w := call(b, "/fail", lease(j)); w.Code != 409 {
		t.Fatal("terminal overwritten", w.Code)
	}
}
func TestWrongJSONAndNoRetryOnTimeout(t *testing.T) {
	for _, sent := range []bool{false, true} {
		t.Run(fmt.Sprint(sent), func(t *testing.T) {
			b := newBridge("127.0.0.1:8791")
			now := time.Now()
			b.now = func() time.Time { return now }
			call(b, "/probes", map[string]int{"count": 1})
			j := claim(t, b, "worker-one")
			if sent {
				call(b, "/send", lease(j))
			}
			now = now.Add(4 * time.Minute)
			if w := call(b, "/claim", map[string]string{"worker": "worker-two"}); w.Code != 204 {
				t.Fatal("expired job requeued", w.Code)
			}
			want := "failed"
			if sent {
				want = "uncertain"
			}
			if b.jobs[0].State != want {
				t.Fatal(b.jobs[0].State)
			}
			if w := call(b, "/send", lease(j)); w.Code != 409 {
				t.Fatal("expired send", w.Code)
			}
		})
	}
	b := newBridge("127.0.0.1:8791")
	call(b, "/probes", map[string]int{"count": 1})
	j := claim(t, b, "worker-one")
	body := lease(j)
	call(b, "/send", body)
	body["reply"] = `{"probe_id":"another-job","ok":true,"text":"Фонарь осветил пустую площадь."}`
	call(b, "/result", body)
	if b.jobs[0].State != "failed" {
		t.Fatal("mixed result accepted")
	}
}
func TestConcurrentClaimsAndProbeLimits(t *testing.T) {
	b := newBridge("127.0.0.1:8791")
	call(b, "/probes", map[string]int{"count": 3})
	if w := call(b, "/probes", map[string]int{"count": 1}); w.Code != 409 {
		t.Fatal("overlapping run")
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := map[string]bool{}
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := call(b, "/claim", map[string]string{"worker": fmt.Sprintf("worker-%03d", i)})
			if w.Code == 204 {
				return
			}
			if w.Code != 200 {
				t.Errorf("claim %d", w.Code)
				return
			}
			var j job
			_ = json.Unmarshal(w.Body.Bytes(), &j)
			mu.Lock()
			defer mu.Unlock()
			if ids[j.ID] {
				t.Error("duplicate assignment")
			}
			ids[j.ID] = true
		}(i)
	}
	wg.Wait()
	if len(ids) != 3 {
		t.Fatal("claims", len(ids))
	}
}
func TestAccessBoundaries(t *testing.T) {
	b := newBridge("127.0.0.1:8791")
	for _, tt := range []struct {
		path, host, origin, auth, site string
		code                           int
	}{
		{"/status", b.host, "", "", "", 401},
		{"/", "attacker.example:8791", "", "", "", 403},
		{"/probes", b.host, "https://attacker.example", "Bearer " + b.token, "", 403},
		{"/", b.host, "", "", "cross-site", 403},
		{"/status", b.host, "", "Bearer " + b.token, "", 200},
		{"/", b.host, "", "", "none", 200},
	} {
		r := httptest.NewRequest("GET", "http://"+tt.host+tt.path, nil)
		r.Header.Set("Origin", tt.origin)
		r.Header.Set("Authorization", tt.auth)
		r.Header.Set("Sec-Fetch-Site", tt.site)
		w := httptest.NewRecorder()
		b.ServeHTTP(w, r)
		if w.Code != tt.code {
			t.Errorf("%s %s got %d want %d", tt.path, tt.host, w.Code, tt.code)
		}
	}
	call(b, "/probes", map[string]int{"count": 1})
	j := claim(t, b, "worker-one")
	r := httptest.NewRequest("GET", "http://"+b.host+"/status", nil)
	r.Header.Set("Authorization", "Bearer "+b.token)
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), j.Lease) {
		t.Fatal("lease leaked into report")
	}
	if w := call(b, "/probes", map[string]int{"count": 2}); w.Code != 400 {
		t.Fatal("invalid count", w.Code)
	}
	if w := call(b, "/claim", map[string]string{"worker": "worker-oversize", "reply": strings.Repeat("x", 70<<10)}); w.Code != 413 {
		t.Fatal("unbounded body", w.Code)
	}
}
func TestReplyValidation(t *testing.T) {
	raw := `{"probe_id":"id","ok":true,"text":"Фонарь осветил пустую площадь."}`
	for _, s := range []string{raw, "```json\n" + raw + "\n```"} {
		if !validReply(s, "id") {
			t.Fatal("valid rejected")
		}
	}
	for _, s := range []string{raw + `{}`, "prefix " + raw, strings.Replace(raw, `"ok":true`, `"ok":false`, 1), strings.Replace(raw, `"ok":true`, `"ok":true,"extra":0`, 1)} {
		if validReply(s, "id") {
			t.Fatal("invalid accepted", s)
		}
	}
}

func TestConfigurableConcurrencyAndWorkerStatus(t *testing.T) {
	b := newBridge("127.0.0.1:8791")
	b.maxActive = 1
	call(b, "/probes", map[string]int{"count": 3})
	j := claim(t, b, "worker-one")
	if w := call(b, "/claim", map[string]string{"worker": "worker-two"}); w.Code != 204 {
		t.Fatal("parallelism not bounded", w.Code)
	}
	if w := call(b, "/heartbeat", map[string]string{"worker": "worker-one", "status": "attention"}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if b.workers["worker-one"].Status != "attention" {
		t.Fatal("attention status missing")
	}
	call(b, "/fail", lease(j))
	_ = claim(t, b, "worker-two")
}

func TestWarmupDelaysOnlyNewLeases(t *testing.T) {
	b := newBridge("127.0.0.1:8791")
	now := time.Now()
	b.now = func() time.Time { return now }
	b.leaseWarmup = 2 * time.Second
	call(b, "/probes", map[string]int{"count": 3})
	if w := call(b, "/claim", map[string]string{"worker": "worker-one"}); w.Code != 204 {
		t.Fatal("lease before warmup", w.Code)
	}
	now = now.Add(time.Second)
	if w := call(b, "/claim", map[string]string{"worker": "worker-one"}); w.Code != 204 {
		t.Fatal("early lease")
	}
	now = now.Add(time.Second)
	j := claim(t, b, "worker-one")
	call(b, "/send", lease(j))
	body := lease(j)
	body["reply"] = fmt.Sprintf(`{"probe_id":%q,"ok":true,"text":"Фонарь осветил пустую площадь."}`, j.ID)
	call(b, "/result", body)
	if w := call(b, "/claim", map[string]string{"worker": "worker-one"}); w.Code != 204 {
		t.Fatal("new page skipped warmup", w.Code)
	}
}
