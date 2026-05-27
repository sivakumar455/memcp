package embedding

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type OllamaProvider struct {
	url       string
	modelName string
	cmd       *exec.Cmd
}

type ollamaEmbeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbeddingResponse struct {
	Embedding []float32 `json:"embedding"`
}

// NewOllamaProvider creates a provider for Ollama.
func NewOllamaProvider(url, modelName string) (*OllamaProvider, error) {
	if url == "" {
		url = "http://localhost:11434"
	}
	if modelName == "" {
		modelName = "nomic-embed-text"
	}
	
	// Normalize URL
	url = strings.TrimSuffix(url, "/")

	p := &OllamaProvider{
		url:       url,
		modelName: modelName,
	}

	// 1. Check if Ollama is running
	if err := p.ping(); err != nil {
		// 2. Not running. Attempt to spawn it.
		fmt.Println("Ollama not detected. Starting ollama serve...")
		p.cmd = exec.Command("ollama", "serve")
		if err := p.cmd.Start(); err != nil {
			return nil, fmt.Errorf("failed to start ollama serve: %w", err)
		}

		// Wait for it to come up
		started := false
		for i := 0; i < 10; i++ {
			time.Sleep(500 * time.Millisecond)
			if err := p.ping(); err == nil {
				started = true
				break
			}
		}

		if !started {
			p.Close()
			return nil, fmt.Errorf("timed out waiting for ollama to start")
		}
	}

	// 3. Ensure the model is pulled
	// If the model exists, this returns instantly. If not, it downloads it.
	fmt.Printf("Ensuring model '%s' is available (this may take a moment if downloading for the first time)...\n", p.modelName)
	pullCmd := exec.Command("ollama", "pull", p.modelName)
	if err := pullCmd.Run(); err != nil {
		fmt.Printf("Warning: failed to auto-pull model '%s': %v\n", p.modelName, err)
		// We don't hard fail here; we let Generate() return the 404 error if it's truly missing.
	}

	return p, nil
}

func (p *OllamaProvider) ping() error {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(p.url + "/")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return nil
}

func (p *OllamaProvider) Generate(text string) ([]float32, error) {
	reqBody := ollamaEmbeddingRequest{
		Model:  p.modelName,
		Prompt: text,
	}
	
	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	resp, err := http.Post(p.url+"/api/embeddings", "application/json", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var res ollamaEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decoding ollama response: %w", err)
	}

	if len(res.Embedding) == 0 {
		return nil, fmt.Errorf("ollama returned empty embedding")
	}

	return res.Embedding, nil
}

func (p *OllamaProvider) Close() error {
	if p.cmd != nil && p.cmd.Process != nil {
		// We spawned it, so we should kill it.
		fmt.Println("Shutting down auto-managed Ollama process...")
		return p.cmd.Process.Kill()
	}
	return nil
}
