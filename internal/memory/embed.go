package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/ethosagent/meterwise/internal/store"
)

// Embedder turns text into a vector of store.EmbedDim values. The HTTP implementation calls our own embeddings
// endpoint; tests and the demo use a deterministic twin on loopback.
type Embedder interface {
	Embed(ctx context.Context, model, text string) ([]float32, Usage, error)
}

// maxEmbedChars caps the text sent to the embedding model.
const maxEmbedChars = 2000

type httpEmbedder struct{ c *modelClient }

// NewEmbedder returns the embedder for an OpenAI-compatible API root and key.
func NewEmbedder(base, key string) Embedder { return httpEmbedder{newModelClient(base, key)} }

type embedRequest struct {
	Model      string `json:"model"`
	Input      string `json:"input"`
	Dimensions int    `json:"dimensions"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Usage struct {
		PromptTokens int64 `json:"prompt_tokens"`
		TotalTokens  int64 `json:"total_tokens"`
	} `json:"usage"`
}

func (e httpEmbedder) Embed(ctx context.Context, model, text string) ([]float32, Usage, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, Usage{}, fmt.Errorf("%w: nothing to embed", errModel)
	}
	var out embedResponse
	err := e.c.post(ctx, "/embeddings", embedRequest{Model: model, Input: cut(text, maxEmbedChars), Dimensions: store.EmbedDim}, &out)
	if err != nil {
		return nil, Usage{}, err
	}
	in := max(out.Usage.PromptTokens, out.Usage.TotalTokens)
	if len(out.Data) == 0 || len(out.Data[0].Embedding) != store.EmbedDim {
		return nil, Usage{Input: in}, fmt.Errorf("%w: embedding has the wrong size", errModel)
	}
	return out.Data[0].Embedding, Usage{Input: in}, nil
}

// cut shortens s to at most n runes.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
