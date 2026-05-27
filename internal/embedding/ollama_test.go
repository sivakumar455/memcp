package embedding

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// isOllamaRunning checks if Ollama is available on the default port.
func isOllamaRunning() bool {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:11434/")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func TestOllamaProvider_Generate(t *testing.T) {
	if !isOllamaRunning() {
		t.Skip("Skipping: Ollama is not running on localhost:11434")
	}

	p, err := NewOllamaProvider("http://localhost:11434", "nomic-embed-text")
	if err != nil {
		t.Fatalf("NewOllamaProvider: %v", err)
	}
	defer p.Close()

	embedding, err := p.Generate("The auth service uses JWT tokens")
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			t.Skip("Skipping: nomic-embed-text model not pulled in Ollama")
		}
		t.Fatalf("Generate: %v", err)
	}

	if len(embedding) == 0 {
		t.Fatal("expected non-empty embedding vector")
	}

	t.Logf("Generated embedding with %d dimensions", len(embedding))
}

func TestOllamaProvider_SemanticSimilarity(t *testing.T) {
	if !isOllamaRunning() {
		t.Skip("Skipping: Ollama is not running on localhost:11434")
	}

	p, err := NewOllamaProvider("http://localhost:11434", "nomic-embed-text")
	if err != nil {
		t.Fatalf("NewOllamaProvider: %v", err)
	}
	defer p.Close()

	// Generate embeddings for semantically similar and dissimilar texts
	embA, err := p.Generate("The authentication token expired causing login failure")
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			t.Skip("Skipping: nomic-embed-text model not pulled in Ollama")
		}
		t.Fatalf("Generate A: %v", err)
	}

	embB, err := p.Generate("User session dies quickly after signing in")
	if err != nil {
		t.Fatalf("Generate B: %v", err)
	}

	embC, err := p.Generate("How to make chocolate pancakes")
	if err != nil {
		t.Fatalf("Generate C: %v", err)
	}

	// Import cosine similarity from the memory package would create a circular dep,
	// so we inline a simple calculation here.
	cosineSim := func(a, b []float32) float32 {
		if len(a) != len(b) {
			return 0
		}
		var dot, normA, normB float32
		for i := range a {
			dot += a[i] * b[i]
			normA += a[i] * a[i]
			normB += b[i] * b[i]
		}
		if normA == 0 || normB == 0 {
			return 0
		}
		import_math := func(x float32) float32 {
			// fast inverse sqrt approximation is overkill; just cast
			return x
		}
		_ = import_math
		// Use float64 for sqrt precision
		return dot / (float32(sqrt64(float64(normA))) * float32(sqrt64(float64(normB))))
	}

	simAB := cosineSim(embA, embB) // semantically related
	simAC := cosineSim(embA, embC) // semantically unrelated

	t.Logf("Similarity (auth-token vs session-dies): %.4f", simAB)
	t.Logf("Similarity (auth-token vs pancakes):     %.4f", simAC)

	if simAB <= simAC {
		t.Errorf("Expected semantically similar texts to score higher: AB=%.4f <= AC=%.4f", simAB, simAC)
	}
}

func sqrt64(x float64) float64 {
	// Newton's method for sqrt, good enough for tests
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 20; i++ {
		z = (z + x/z) / 2
	}
	return z
}
