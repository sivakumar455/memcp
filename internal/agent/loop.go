package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/sivakumar455/memcp/internal/llm"
)

const defaultMaxIterations = 15

// ToolExecutor executes a tool call and returns the result.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, argsJSON string) (string, error)
	ListTools() []llm.ToolDef
}

// Loop runs the agent loop: send to LLM, if tool calls execute and re-send, repeat until done.
type Loop struct {
	provider      llm.Provider
	toolExecutor  ToolExecutor
	maxIterations int
}

func NewLoop(provider llm.Provider, executor ToolExecutor) *Loop {
	return &Loop{
		provider:      provider,
		toolExecutor:  executor,
		maxIterations: defaultMaxIterations,
	}
}

type RunResult struct {
	FinalResponse string
	ToolsUsed     []string
	Iterations    int
}

// Run executes the full agent loop with system context and user message.
func (l *Loop) Run(ctx context.Context, systemPrompt, userMessage string) (*RunResult, error) {
	loopStart := time.Now()
	slog.Info("Agent loop starting",
		"provider", l.provider.Name(),
		"max_iterations", l.maxIterations,
		"system_prompt_len", len(systemPrompt),
		"user_message_len", len(userMessage),
	)

	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMessage},
	}

	tools := l.toolExecutor.ListTools()
	slog.Info("Tools available for agent", "count", len(tools))

	var toolsUsed []string

	for i := 0; i < l.maxIterations; i++ {
		iterStart := time.Now()
		slog.Info("Agent iteration starting", "iteration", i+1, "messages", len(messages))

		resp, err := l.provider.Complete(ctx, &llm.CompletionRequest{
			Messages: messages,
			Tools:    tools,
		})
		if err != nil {
			slog.Error("LLM completion failed", "iteration", i+1, "error", err, "elapsed_ms", time.Since(iterStart).Milliseconds())
			return nil, fmt.Errorf("iteration %d: LLM error: %w", i+1, err)
		}

		slog.Info("LLM response received",
			"iteration", i+1,
			"response_len", len(resp.Content),
			"tool_calls", len(resp.ToolCalls),
			"done", resp.Done,
			"llm_elapsed_ms", time.Since(iterStart).Milliseconds(),
		)

		assistantMsg := llm.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		messages = append(messages, assistantMsg)

		if resp.Done || len(resp.ToolCalls) == 0 {
			slog.Info("Agent loop complete",
				"iterations", i+1,
				"tools_used", len(toolsUsed),
				"response_len", len(resp.Content),
				"total_elapsed_ms", time.Since(loopStart).Milliseconds(),
			)
			return &RunResult{
				FinalResponse: resp.Content,
				ToolsUsed:     toolsUsed,
				Iterations:    i + 1,
			}, nil
		}

		for _, tc := range resp.ToolCalls {
			toolStart := time.Now()
			slog.Info("Executing tool call", "tool", tc.Name, "call_id", tc.ID, "args_len", len(tc.Arguments))
			toolsUsed = append(toolsUsed, tc.Name)

			result, err := l.toolExecutor.Execute(ctx, tc.Name, tc.Arguments)
			if err != nil {
				result = fmt.Sprintf("Error executing %s: %v", tc.Name, err)
				slog.Warn("Tool execution failed", "tool", tc.Name, "error", err, "elapsed_ms", time.Since(toolStart).Milliseconds())
			} else {
				slog.Info("Tool execution succeeded", "tool", tc.Name, "result_len", len(result), "elapsed_ms", time.Since(toolStart).Milliseconds())
			}

			if len(result) > 50000 {
				origLen := len(result)
				result = result[:50000] + "\n\n... [truncated, result was " + fmt.Sprintf("%d", origLen) + " chars]"
				slog.Warn("Tool result truncated", "tool", tc.Name, "original_len", origLen, "truncated_to", 50000)
			}

			messages = append(messages, llm.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}

		slog.Info("Agent iteration complete", "iteration", i+1, "elapsed_ms", time.Since(iterStart).Milliseconds())
	}

	slog.Warn("Agent reached max iterations", "max", l.maxIterations, "tools_used", len(toolsUsed), "total_elapsed_ms", time.Since(loopStart).Milliseconds())

	lastContent := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && messages[i].Content != "" {
			lastContent = messages[i].Content
			break
		}
	}

	return &RunResult{
		FinalResponse: lastContent + "\n\n[Agent reached maximum iterations (" + fmt.Sprintf("%d", l.maxIterations) + ")]",
		ToolsUsed:     toolsUsed,
		Iterations:    l.maxIterations,
	}, nil
}

// RunSimple runs without tool calling -- just a single LLM call with context.
func (l *Loop) RunSimple(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	start := time.Now()
	slog.Info("RunSimple starting", "provider", l.provider.Name(), "prompt_len", len(systemPrompt), "message_len", len(userMessage))

	resp, err := l.provider.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMessage},
		},
	})
	if err != nil {
		slog.Error("RunSimple failed", "error", err, "elapsed_ms", time.Since(start).Milliseconds())
		return "", err
	}

	slog.Info("RunSimple completed", "response_len", len(resp.Content), "elapsed_ms", time.Since(start).Milliseconds())
	return resp.Content, nil
}

// FormatToolCallSummary returns a human-readable summary of tools used.
func FormatToolCallSummary(result *RunResult) string {
	if len(result.ToolsUsed) == 0 {
		return fmt.Sprintf("Completed in %d iteration(s), no tools used.", result.Iterations)
	}

	counts := make(map[string]int)
	for _, t := range result.ToolsUsed {
		counts[t]++
	}

	var parts []string
	for name, count := range counts {
		if count > 1 {
			parts = append(parts, fmt.Sprintf("%s (x%d)", name, count))
		} else {
			parts = append(parts, name)
		}
	}

	return fmt.Sprintf("Completed in %d iteration(s). Tools used: %s",
		result.Iterations, strings.Join(parts, ", "))
}

// ParseToolArgs is a helper to unmarshal tool call arguments.
func ParseToolArgs(argsJSON string, target any) error {
	return json.Unmarshal([]byte(argsJSON), target)
}
