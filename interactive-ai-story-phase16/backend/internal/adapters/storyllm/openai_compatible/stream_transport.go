package openai_compatible

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
)

var errStreamUnsupported = errors.New("provider explicitly rejected streaming")
var errSSEFinished = errors.New("SSE terminal marker")

func streamUnsupported(status int, raw []byte) bool {
	if status != 400 && status != 422 {
		return false
	}
	text := strings.ToLower(string(raw))
	if !strings.Contains(text, "stream") {
		return false
	}
	for _, marker := range []string{"unsupported", "not supported", "not allowed", "unknown field", "unknown name", "unrecognized"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// Credential retries happen only before a stream is accepted. A partial stream
// is never concatenated with a replacement request or a different credential.
func (c *Client) openWriterStream(ctx context.Context, body []byte, agent bool) (*http.Response, error) {
	return c.request(ctx, body, agent, true)
}

func consumeWriterSSE(ctx context.Context, reader io.Reader, handle func(string, []byte) error) error {
	scanner := bufio.NewScanner(io.LimitReader(reader, (4<<20)+1))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var name string
	var data bytes.Buffer
	total := 0
	dispatch := func() error {
		if data.Len() == 0 {
			return nil
		}
		raw := bytes.TrimSuffix(data.Bytes(), []byte("\n"))
		err := handle(name, raw)
		data.Reset()
		name = ""
		return err
	}
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Text()
		total += len(line) + 1
		if total > 4<<20 {
			return errors.New("writer SSE exceeds limit")
		}
		if line == "" {
			if err := dispatch(); err != nil {
				if errors.Is(err, errSSEFinished) {
					return nil
				}
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(line, "data:")
			value = strings.TrimPrefix(value, " ")
			data.WriteString(value)
			data.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := dispatch(); err != nil && !errors.Is(err, errSSEFinished) {
		return err
	}
	return nil
}
