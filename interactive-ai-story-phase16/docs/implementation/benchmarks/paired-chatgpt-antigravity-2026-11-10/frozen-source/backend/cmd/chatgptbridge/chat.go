package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// This endpoint is experimental and never registered in the story application's router.
// Browser UI settings do not expose temperature, token accounting or native schemas.
type completionRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Stream          bool            `json:"stream"`
	Temperature     float64         `json:"temperature"`
	TopP            float64         `json:"top_p"`
	MaxTokens       int             `json:"max_tokens"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
	ResponseFormat  json.RawMessage `json:"response_format"`
}

func jsonReply(reply string) (string, bool) {
	s := strings.TrimSpace(reply)
	if strings.HasPrefix(s, "```") && strings.HasSuffix(s, "```") {
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "```"), "```"))
		if strings.HasPrefix(s, "json\n") {
			s = strings.TrimSpace(strings.TrimPrefix(s, "json\n"))
		}
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(s), &obj) != nil || obj == nil {
		return "", false
	}
	return s, true
}

// Preserve message strings while keeping Markdown syntax out of rendered user
// text. ChatGPT renders backticks as inline code, so DOM text otherwise differs.
func renderSafeJSON(raw []byte) (string, error) {
	const digits = "0123456789abcdef"
	var out strings.Builder
	for i := 0; i < len(raw); {
		if raw[i] != '"' {
			out.WriteByte(raw[i])
			i++
			continue
		}
		start := i
		i++
		for i < len(raw) {
			if raw[i] == '\\' {
				i += 2
				continue
			}
			if raw[i] == '"' {
				break
			}
			i++
		}
		if i >= len(raw) {
			return "", errors.New("unterminated JSON string")
		}
		var text string
		if err := json.Unmarshal(raw[start:i+1], &text); err != nil {
			return "", err
		}
		i++
		out.WriteByte('"')
		for _, r := range text {
			punctuation := r >= 33 && r <= 126 && !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
			if r < 32 || punctuation || r == 0x2028 || r == 0x2029 {
				out.WriteString("\\u")
				for shift := 12; shift >= 0; shift -= 4 {
					out.WriteByte(digits[(r>>shift)&15])
				}
			} else {
				out.WriteRune(r)
			}
		}
		out.WriteByte('"')
	}
	return out.String(), nil
}

func (b *bridge) chatCompletions(w http.ResponseWriter, r *http.Request) {
	var in completionRequest
	if !decode(w, r, &in) {
		return
	}
	if in.Stream || in.Model != "gpt-6-instant" || len(in.Messages) < 1 || len(in.Messages) > 8 {
		http.Error(w, "only buffered gpt-6-instant with 1..8 text messages is supported", 400)
		return
	}
	for _, m := range in.Messages {
		if (m.Role != "system" && m.Role != "user") || strings.TrimSpace(m.Content) == "" {
			http.Error(w, "only nonempty system/user messages are supported", 400)
			return
		}
	}
	// One physical line survives contenteditable insertion unchanged. The JSON
	// envelope preserves every original message newline and role as data.
	payload, err := json.Marshal(map[string]any{"messages": in.Messages, "response_format": in.ResponseFormat})
	if err != nil {
		http.Error(w, "invalid response format", 400)
		return
	}
	encoded, err := renderSafeJSON(payload)
	if err != nil {
		http.Error(w, "invalid transport envelope", 400)
		return
	}
	prompt := "Execute this isolated story-generation test. Decode MESSAGES_JSON below, follow its system messages as instructions and its user messages as input data, in order. Return exactly one JSON object, without Markdown or explanations. Do not browse or use tools. MESSAGES_JSON=" + encoded
	if len(prompt) > 48<<10 {
		http.Error(w, "prompt exceeds 48 KiB", 413)
		return
	}
	b.mu.Lock()
	b.expire()
	pending := 0
	for _, j := range b.jobs {
		if active(j.State) {
			pending++
		}
	}
	if pending >= 12 {
		b.mu.Unlock()
		http.Error(w, "test queue is full", 429)
		return
	}
	// Retain active jobs while bounding history.
	if len(b.jobs) >= 60 {
		for i, j := range b.jobs {
			if !active(j.State) {
				b.jobs = append(b.jobs[:i], b.jobs[i+1:]...)
				break
			}
		}
	}
	j := &job{RecoverSafeFailure: r.Header.Get("X-Story-Benchmark") == "paired-v1", ID: secret(), Prompt: prompt, Kind: "chat", State: "queued", Created: b.now(), Deadline: b.now().Add(4 * time.Minute)}
	b.jobs = append(b.jobs, j)
	b.mu.Unlock()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			b.mu.Lock()
			if active(j.State) {
				now := b.now()
				j.Finished = &now
				if j.State == "dispatched" {
					j.State = "uncertain"
				} else {
					j.State = "failed"
				}
				j.Error = "request caller cancelled; no automatic retry"
			}
			b.mu.Unlock()
			return
		case <-ticker.C:
			b.mu.Lock()
			b.expire()
			state, reply := j.State, j.Reply
			b.mu.Unlock()
			if active(state) {
				continue
			}
			if state != "complete" {
				http.Error(w, "browser test job "+state+"; inspect local dashboard", 502)
				return
			}
			normalized, ok := jsonReply(reply)
			if !ok {
				http.Error(w, "invalid JSON response", 502)
				return
			}
			writeJSON(w, map[string]any{"id": j.ID, "object": "chat.completion", "model": "gpt-6-instant", "choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": normalized}, "finish_reason": "stop"}}})
			return
		}
	}
}

func tokenFile(path string, fallback string) (string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return "", err
		}
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return "", e
		}
		_, e = f.WriteString(fallback + "\n")
		closeErr := f.Close()
		if e != nil {
			return "", e
		}
		return fallback, closeErr
	}
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Mode().Perm()&0077 != 0 {
		return "", errors.New("bridge token file must be owner-only")
	}
	token := strings.TrimSpace(string(raw))
	decoded, err := hex.DecodeString(token)
	if err != nil || len(decoded) != 24 {
		return "", errors.New("invalid local bridge token file")
	}
	return token, nil
}
