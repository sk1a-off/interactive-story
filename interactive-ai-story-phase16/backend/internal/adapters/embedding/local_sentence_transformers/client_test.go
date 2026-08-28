package local_sentence_transformers

import (
	"context"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"io"
	"net/http"
	"strings"
	"testing"
)

type rt func(*http.Request) (*http.Response, error)

func (f rt) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestQueryContractRequires384Dimensions(t *testing.T) {
	vec := strings.TrimSuffix(strings.Repeat("0,", 384), ",")
	hc := &http.Client{Transport: rt(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"mode":"query"`) {
			t.Fatal("query mode missing")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"vectors":[[` + vec + `]]}`)), Header: make(http.Header)}, nil
	})}
	c, _ := New("http://127.0.0.1:8090", "deepvk/USER2-small", hc)
	out, e := c.EmbedQueries(context.Background(), aiport.EmbeddingRequest{Texts: []string{"hello"}})
	if e != nil {
		t.Fatal(e)
	}
	if len(out.Vectors[0]) != 384 {
		t.Fatal("wrong dimension")
	}
}
