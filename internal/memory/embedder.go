// Package memory — Vertex AI embedder and fallback implementations.
package memory

import (
	"context"
	"fmt"
	"log/slog"

	aiplatform "cloud.google.com/go/aiplatform/apiv1"
	aiplatformpb "cloud.google.com/go/aiplatform/apiv1/aiplatformpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/structpb"
)

// ─── VERTEX AI EMBEDDER ─────────────────────────────────────────────────────

// VertexEmbedder generates embeddings using Vertex AI text-embedding-004.
// 768 dimensions — matches the existing Ziloss pgvector infrastructure.
type VertexEmbedder struct {
	client   *aiplatform.PredictionClient
	endpoint string // full resource path to the model
	logger   *slog.Logger
}

// NewVertexEmbedder creates an embedder using Vertex AI.
// project: GCP project ID (e.g. "my-project")
// location: GCP region (e.g. "us-central1")
func NewVertexEmbedder(ctx context.Context, project, location string, logger *slog.Logger) (*VertexEmbedder, error) {
	apiEndpoint := fmt.Sprintf("%s-aiplatform.googleapis.com:443", location)
	client, err := aiplatform.NewPredictionClient(ctx,
		option.WithEndpoint(apiEndpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("memory.NewVertexEmbedder: %w", err)
	}

	endpoint := fmt.Sprintf(
		"projects/%s/locations/%s/publishers/google/models/text-embedding-004",
		project, location,
	)

	return &VertexEmbedder{
		client:   client,
		endpoint: endpoint,
		logger:   logger,
	}, nil
}

// Embed generates a 768-dimensional embedding for the given text.
func (e *VertexEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	instance, err := structpb.NewValue(map[string]interface{}{
		"content": text,
	})
	if err != nil {
		return nil, fmt.Errorf("memory.VertexEmbedder.Embed structpb: %w", err)
	}

	resp, err := e.client.Predict(ctx, &aiplatformpb.PredictRequest{
		Endpoint:  e.endpoint,
		Instances: []*structpb.Value{instance},
	})
	if err != nil {
		return nil, fmt.Errorf("memory.VertexEmbedder.Embed predict: %w", err)
	}

	if len(resp.Predictions) == 0 {
		return nil, fmt.Errorf("memory.VertexEmbedder.Embed: no predictions returned")
	}

	// Parse: predictions[0].embeddings.values
	pred := resp.Predictions[0].GetStructValue()
	if pred == nil {
		return nil, fmt.Errorf("memory.VertexEmbedder.Embed: prediction is not a struct")
	}

	embeddingsField, ok := pred.Fields["embeddings"]
	if !ok {
		return nil, fmt.Errorf("memory.VertexEmbedder.Embed: no 'embeddings' field")
	}

	embeddingsStruct := embeddingsField.GetStructValue()
	if embeddingsStruct == nil {
		return nil, fmt.Errorf("memory.VertexEmbedder.Embed: 'embeddings' is not a struct")
	}

	valuesField, ok := embeddingsStruct.Fields["values"]
	if !ok {
		return nil, fmt.Errorf("memory.VertexEmbedder.Embed: no 'values' field")
	}

	valuesList := valuesField.GetListValue()
	if valuesList == nil {
		return nil, fmt.Errorf("memory.VertexEmbedder.Embed: 'values' is not a list")
	}

	embedding := make([]float32, len(valuesList.Values))
	for i, v := range valuesList.Values {
		embedding[i] = float32(v.GetNumberValue())
	}

	e.logger.Debug("vertex embedding generated", "dims", len(embedding), "text_len", len(text))
	return embedding, nil
}

// Dims returns the embedding dimensionality.
func (e *VertexEmbedder) Dims() int { return 768 }

// Close releases the underlying gRPC connection.
func (e *VertexEmbedder) Close() error {
	return e.client.Close()
}

// ─── NOOP EMBEDDER (FALLBACK) ───────────────────────────────────────────────

// NoopEmbedder returns zero vectors. Used when Vertex AI is unavailable.
// Memory search falls back to BM25-only when embeddings are all zeros.
type NoopEmbedder struct{}

func (NoopEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, 768), nil
}

func (NoopEmbedder) Dims() int { return 768 }
