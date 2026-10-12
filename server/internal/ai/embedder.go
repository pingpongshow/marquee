package ai

import (
	"context"
	"errors"
	"fmt"
)

// DocEmbedder is the movie and show text embedder interface (items.TextEmbedder).
type DocEmbedder interface {
	EmbedDocs(ctx context.Context, texts []string, kind string) (string, [][]float32, error)
}

// SwitchEmbedder embeds movie and show descriptions with Ollama's embedding model while AI
// is turned on, and with Fallback (the Soundprint sidecar's bge) while it's off. When AI
// is on but Ollama is down it fails rather than mixing in another model's vectors (the
// caller then ranks by rating, and the index task waits).
type SwitchEmbedder struct {
	AI       *Client
	Fallback DocEmbedder
}

func (s *SwitchEmbedder) EmbedDocs(ctx context.Context, texts []string, kind string) (string, [][]float32, error) {
	if s.AI == nil || !s.AI.Enabled() {
		if s.Fallback == nil {
			return "", nil, errors.New("no text embedder")
		}
		return s.Fallback.EmbedDocs(ctx, texts, kind)
	}
	model := s.AI.EmbedModel()
	if len(texts) == 0 {
		// Report the model id; check Ollama answers so the index task can wait when it doesn't.
		if !s.AI.Ready() {
			return "", nil, ErrUnavailable
		}
		if st := s.AI.Check(ctx, s.AI.cfg()); !st.Reachable || !st.EmbedInstalled {
			if st.Error == "" {
				st.Error = fmt.Sprintf("the model %s isn't installed in Ollama", model)
			}
			return "", nil, fmt.Errorf("%w: %s", ErrUnavailable, st.Error)
		}
		return ModelID(model), nil, nil
	}
	prefix := DocPrefix(model)
	if kind == "query" {
		prefix = QueryPrefix(model)
	}
	in := texts
	if prefix != "" {
		in = make([]string, len(texts))
		for i, t := range texts {
			in[i] = prefix + t
		}
	}
	vs, err := s.AI.Embed(ctx, in)
	return ModelID(model), vs, err
}
