package daemon

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// PromptBuilderFunc builds a custom prompt for a given task.
// Extensions register these to override the default generic prompt for
// specific source/category combinations.
type PromptBuilderFunc func(task Task) string

var (
	promptBuildersMu sync.RWMutex
	promptBuilders   = map[string]PromptBuilderFunc{}
)

// RegisterPromptBuilder registers a custom prompt builder for a given
// source+category key (e.g. "bitbucket:pull_request"). When buildTaskPrompt
// encounters a task matching this key, it delegates to the registered builder.
func RegisterPromptBuilder(source, category string, fn PromptBuilderFunc) {
	promptBuildersMu.Lock()
	defer promptBuildersMu.Unlock()
	promptBuilders[source+":"+category] = fn
}

// CursorAgent dispatches tasks to the Cursor `agent` CLI for autonomous execution.
type CursorAgent struct {
	cursorPath string
	timeout    time.Duration
	workDir    string
}

// CursorAgentConfig controls the Cursor CLI agent integration.
type CursorAgentConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	CursorPath string `mapstructure:"cursor_path"`
	Timeout    int    `mapstructure:"timeout"`
	WorkDir    string `mapstructure:"work_dir"`
}

// NewCursorAgent creates a CursorAgent from config.
func NewCursorAgent(cfg CursorAgentConfig) *CursorAgent {
	path := cfg.CursorPath
	if path == "" {
		path = findAgentCLI()
	}
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	return &CursorAgent{
		cursorPath: path,
		timeout:    timeout,
		workDir:    cfg.WorkDir,
	}
}

func findAgentCLI() string {
	if path, err := exec.LookPath("agent"); err == nil {
		return path
	}
	if runtime.GOOS == "darwin" {
		candidates := []string{
			"/usr/local/bin/agent",
			"/Applications/Cursor.app/Contents/Resources/app/bin/cursor",
		}
		for _, p := range candidates {
			if _, err := exec.LookPath(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// Available returns whether the agent CLI was found.
func (ca *CursorAgent) Available() bool {
	return ca.cursorPath != ""
}

// Path returns the resolved CLI path.
func (ca *CursorAgent) Path() string {
	return ca.cursorPath
}

// Run sends a prompt to the agent CLI in headless mode.
func (ca *CursorAgent) Run(ctx context.Context, prompt string) (string, error) {
	if ca.cursorPath == "" {
		return "", fmt.Errorf("agent CLI not found")
	}

	ctx, cancel := context.WithTimeout(ctx, ca.timeout)
	defer cancel()

	args := []string{
		"--print",
		"--approve-mcps",
		"--force",
	}
	if ca.workDir != "" {
		args = append(args, "--workspace", ca.workDir)
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, ca.cursorPath, args...)
	if ca.workDir != "" {
		cmd.Dir = ca.workDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	slog.Info("CursorAgent executing", "path", ca.cursorPath, "prompt_len", len(prompt), "timeout", ca.timeout)
	start := time.Now()

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("agent timed out after %s", ca.timeout)
		}
		return "", fmt.Errorf("agent failed: %w\nstderr: %s", err, stderr.String())
	}

	result := strings.TrimSpace(stdout.String())
	slog.Info("CursorAgent completed", "result_len", len(result), "elapsed_ms", time.Since(start).Milliseconds())
	return result, nil
}

// RunTask builds a prompt from a daemon task and executes it via the agent CLI.
func (ca *CursorAgent) RunTask(ctx context.Context, task Task) (string, error) {
	prompt := buildTaskPrompt(task)
	return ca.Run(ctx, prompt)
}

func buildTaskPrompt(task Task) string {
	promptBuildersMu.RLock()
	fn, ok := promptBuilders[task.Source+":"+task.Category]
	promptBuildersMu.RUnlock()
	if ok {
		return fn(task)
	}

	var b strings.Builder
	b.WriteString("You are investigating a task from the memcp daemon.\n\n")
	b.WriteString(fmt.Sprintf("Source: %s\n", task.Source))
	b.WriteString(fmt.Sprintf("ID: %s\n", task.SourceID))
	b.WriteString(fmt.Sprintf("Title: %s\n", task.Title))
	b.WriteString(fmt.Sprintf("Priority: %s\n", task.Priority))
	if task.Category != "" {
		b.WriteString(fmt.Sprintf("Category: %s\n", task.Category))
	}
	b.WriteString(fmt.Sprintf("\nSummary:\n%s\n", task.Summary))
	if task.RawData != "" {
		b.WriteString(fmt.Sprintf("\nRaw data:\n%s\n", task.RawData))
	}
	b.WriteString("\nYou have full MCP tool access. Use the following approach:\n")
	b.WriteString("1. Call agent_recall to load relevant memory and context.\n")
	b.WriteString("2. Use your configured MCP backend tools as needed for the investigation.\n")
	b.WriteString("3. Search the codebase for relevant code, configurations, or logs.\n")
	b.WriteString("4. Provide a concise analysis with findings and recommended next steps.\n")
	b.WriteString("5. Call agent_save to persist important findings for future sessions.\n")
	return b.String()
}
