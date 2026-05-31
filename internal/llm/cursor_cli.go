package llm

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// CursorCLI invokes the Cursor IDE CLI to get LLM completions.
type CursorCLI struct {
	timeout time.Duration
}

func NewCursorCLI(timeout time.Duration) *CursorCLI {
	return &CursorCLI{timeout: timeout}
}

func (c *CursorCLI) Name() string { return "cursor_cli" }

func (c *CursorCLI) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	prompt := buildPromptFromMessages(req.Messages)

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	result, err := c.runCursorCommand(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("cursor CLI: %w", err)
	}

	return &CompletionResponse{
		Content: result,
		Done:    true,
	}, nil
}

func (c *CursorCLI) runCursorCommand(ctx context.Context, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, "cursor", "--chat", prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	slog.Debug("Running cursor CLI", "promptLen", len(prompt))

	if err := cmd.Run(); err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			return "", fmt.Errorf("cursor CLI not found in PATH. Is Cursor installed? " +
				"For autonomous mode, configure an OpenAI-compatible provider (e.g. Ollama) instead")
		}
		return "", fmt.Errorf("cursor CLI failed: %w\nstderr: %s", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

func buildPromptFromMessages(msgs []Message) string {
	var parts []string
	for _, m := range msgs {
		switch m.Role {
		case "system":
			parts = append(parts, m.Content)
		case "user":
			parts = append(parts, "\nUser: "+m.Content)
		case "assistant":
			parts = append(parts, "\nAssistant: "+m.Content)
		case "tool":
			parts = append(parts, "\nTool result: "+m.Content)
		}
	}
	return strings.Join(parts, "\n")
}
