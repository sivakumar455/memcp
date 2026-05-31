package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPClient connects to an external MCP server via stdio.
type MCPClient struct {
	name    string
	command string
	args    []string
	env     map[string]string
	proxy   bool
	session *gomcp.ClientSession
	tools   map[string]*gomcp.Tool
	mu      sync.RWMutex
	healthy bool
}

func NewMCPClient(name, command string, args []string, env map[string]string) *MCPClient {
	return &MCPClient{
		name:    name,
		command: command,
		args:    args,
		env:     env,
		proxy:   true,
		tools:   make(map[string]*gomcp.Tool),
	}
}

func (c *MCPClient) Name() string      { return c.name }
func (c *MCPClient) ShouldProxy() bool { return c.proxy }
func (c *MCPClient) SetProxy(v bool)   { c.proxy = v }

func (c *MCPClient) Connect(ctx context.Context) error {
	start := time.Now()
	slog.Info("MCP client connecting", "server", c.name, "command", c.command, "args", c.args)

	cmd := exec.CommandContext(ctx, c.command, c.args...)

	if len(c.env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range c.env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	transport := &gomcp.CommandTransport{Command: cmd}

	client := gomcp.NewClient(&gomcp.Implementation{
		Name:    "memcp-client",
		Version: "2.0",
	}, nil)

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", c.name, err)
	}

	c.session = session
	c.healthy = true
	slog.Info("MCP client session established", "server", c.name, "elapsed_ms", time.Since(start).Milliseconds())

	if err := c.discoverTools(ctx); err != nil {
		slog.Warn("Failed to discover tools", "server", c.name, "error", err)
	}

	slog.Info("MCP client ready", "server", c.name, "tools", len(c.tools), "elapsed_ms", time.Since(start).Milliseconds())
	return nil
}

func (c *MCPClient) discoverTools(ctx context.Context) error {
	result, err := c.session.ListTools(ctx, nil)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, tool := range result.Tools {
		c.tools[tool.Name] = tool
	}
	slog.Info("Tools discovered", "server", c.name, "count", len(result.Tools))
	return nil
}

func (c *MCPClient) HasTool(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.tools[name]
	return ok
}

func (c *MCPClient) RawTools() []*gomcp.Tool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var result []*gomcp.Tool
	for _, t := range c.tools {
		result = append(result, t)
	}
	return result
}

// CallToolRaw invokes a tool with map args and returns the raw MCP result.
func (c *MCPClient) CallToolRaw(ctx context.Context, name string, args map[string]any) (*gomcp.CallToolResult, error) {
	if c.session == nil {
		return nil, fmt.Errorf("MCP client %s not connected", c.name)
	}

	start := time.Now()
	result, err := c.session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		slog.Error("CallToolRaw failed", "server", c.name, "tool", name, "error", err, "elapsed_ms", time.Since(start).Milliseconds())
		return nil, fmt.Errorf("call tool %s: %w", name, err)
	}

	slog.Debug("CallToolRaw completed", "server", c.name, "tool", name, "elapsed_ms", time.Since(start).Milliseconds())
	return result, nil
}

// Healthy returns whether the client's last interaction was successful.
func (c *MCPClient) Healthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// Ping sends a tools/list request to verify the subprocess is alive.
func (c *MCPClient) Ping(ctx context.Context) error {
	if c.session == nil {
		c.mu.Lock()
		c.healthy = false
		c.mu.Unlock()
		return fmt.Errorf("not connected")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := c.session.ListTools(ctx, nil)
	c.mu.Lock()
	c.healthy = err == nil
	c.mu.Unlock()
	return err
}

// Reconnect closes the old session and re-establishes a new connection.
func (c *MCPClient) Reconnect(ctx context.Context) error {
	slog.Info("Reconnecting MCP client", "server", c.name)
	_ = c.Close()
	c.mu.Lock()
	c.session = nil
	c.tools = make(map[string]*gomcp.Tool)
	c.healthy = false
	c.mu.Unlock()
	return c.Connect(ctx)
}

func (c *MCPClient) Close() error {
	slog.Info("Closing MCP client", "server", c.name)
	c.mu.Lock()
	c.healthy = false
	c.mu.Unlock()
	if c.session != nil {
		return c.session.Close()
	}
	return nil
}
