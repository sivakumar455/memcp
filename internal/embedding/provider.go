package embedding

import (
	"fmt"

	"github.com/sivakumar455/memcp/internal/config"
)

// Provider defines the interface for converting text into vector embeddings.
type Provider interface {
	Generate(text string) ([]float32, error)
	Close() error
}

// NewProvider creates a new embedding provider based on the configuration.
func NewProvider(cfg config.VectorSearchConfig) (Provider, error) {
	if !cfg.Enabled {
		return nil, nil // No provider if disabled
	}

	switch cfg.Provider {
	case "ollama":
		return NewOllamaProvider(cfg.OllamaURL, cfg.ModelName)
	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s", cfg.Provider)
	}
}
