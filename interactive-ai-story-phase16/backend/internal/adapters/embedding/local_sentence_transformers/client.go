package local_sentence_transformers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL  string
	http     *http.Client
	identity aiport.ProviderIdentity
	dims     int
}

func New(baseURL, model string, hc *http.Client) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(model) == "" {
		return nil, errors.New("embedding base URL and model are required")
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: hc, identity: aiport.ProviderIdentity{Kind: aiport.KindEmbedding, Provider: "local_sentence_transformers", Model: model, Profile: "cpu"}, dims: 384}, nil
}
func (c *Client) Identity() aiport.ProviderIdentity { return c.identity }
func (c *Client) Dimensions() int                   { return c.dims }
func (c *Client) Capabilities(context.Context) (aiport.EmbeddingCapabilities, error) {
	return aiport.EmbeddingCapabilities{Dimensions: 384, Device: "cpu", Model: c.identity.Model}, nil
}
func (c *Client) EmbedDocuments(ctx context.Context, r aiport.EmbeddingRequest) (aiport.EmbeddingResponse, error) {
	return c.embed(ctx, "document", r)
}
func (c *Client) EmbedQueries(ctx context.Context, r aiport.EmbeddingRequest) (aiport.EmbeddingResponse, error) {
	return c.embed(ctx, "query", r)
}
func (c *Client) embed(ctx context.Context, kind string, r aiport.EmbeddingRequest) (aiport.EmbeddingResponse, error) {
	raw, _ := json.Marshal(map[string]any{"mode": kind, "texts": r.Texts})
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embed", bytes.NewReader(raw))
	if e != nil {
		return aiport.EmbeddingResponse{}, e
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := c.http.Do(req)
	if e != nil {
		return aiport.EmbeddingResponse{}, fmt.Errorf("%w: %v", aiport.ErrUnavailable, e)
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if e != nil {
		return aiport.EmbeddingResponse{}, e
	}
	if resp.StatusCode != 200 {
		return aiport.EmbeddingResponse{}, fmt.Errorf("%w: status %d", aiport.ErrUnavailable, resp.StatusCode)
	}
	var out struct {
		Vectors [][]float32 `json:"vectors"`
	}
	if e = json.Unmarshal(b, &out); e != nil {
		return aiport.EmbeddingResponse{}, aiport.ErrInvalidOutput
	}
	for _, v := range out.Vectors {
		if len(v) != 384 {
			return aiport.EmbeddingResponse{}, aiport.ErrInvalidOutput
		}
	}
	if len(out.Vectors) != len(r.Texts) {
		return aiport.EmbeddingResponse{}, aiport.ErrInvalidOutput
	}
	return aiport.EmbeddingResponse{Vectors: out.Vectors}, nil
}
