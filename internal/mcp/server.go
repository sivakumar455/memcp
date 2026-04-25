// Package mcpserver implements the MCP tool server for memcp.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sivakumar455/memcp/internal/engine"
	"github.com/sivakumar455/memcp/internal/observation"
	"github.com/sivakumar455/memcp/internal/skills"
)

// Server wraps the MCP server and engine.
type Server struct {
	engine    *engine.Engine
	mcpServer *mcp.Server
	observer  *observation.Observer
}

// New creates a new MCP server with all memcp tools registered.
func New(eng *engine.Engine, version string) *Server {
	s := &Server{engine: eng}

	if eng.Cfg.Observation.Enabled {
		s.observer = observation.New(eng.Store, eng.MemMgr, eng.Cfg.Observation, eng.Profiler)
	}

	mcpSrv := mcp.NewServer(
		&mcp.Implementation{
			Name:    "memcp",
			Version: version,
		},
		nil,
	)

	s.mcpServer = mcpSrv
	s.registerTools()

	return s
}

// Run starts the MCP server over stdio.
func (s *Server) Run(ctx context.Context) error {
	slog.Info("starting MCP server over stdio")
	return s.mcpServer.Run(ctx, &mcp.StdioTransport{})
}

// --- Helper constructors ---

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func errorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: true,
	}
}

// wrapObserver wraps an MCP tool handler with telemetry observation for standalone mode.
func wrapObserver[T any](s *Server, name string, handler func(context.Context, *mcp.CallToolRequest, T) (*mcp.CallToolResult, any, error)) func(context.Context, *mcp.CallToolRequest, T) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, input T) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		res, payload, err := handler(ctx, req, input)

		if s.observer != nil {
			argsRaw, _ := json.Marshal(input)
			resText := ""
			if res != nil && len(res.Content) > 0 {
				if tc, ok := res.Content[0].(*mcp.TextContent); ok {
					resText = tc.Text
				}
			}
			if err != nil {
				resText += "\nError: " + err.Error()
			}
			s.observer.Observe(name, "memcp-standalone", string(argsRaw), resText, int(time.Since(start).Milliseconds()))
		}
		return res, payload, err
	}
}

// registerTools registers all memcp MCP tools.
func (s *Server) registerTools() {
	// agent_recall
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "agent_recall",
		Description: "Recall relevant context from persistent memory. Call this FIRST at the start of every conversation to load cached findings, persona, and session history.",
	}, wrapObserver(s, "agent_recall", s.handleRecall))

	// agent_save
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "agent_save",
		Description: "Save a finding to persistent memory. Use for significant discoveries: root causes, environment states, important decisions. Do NOT save every intermediate step.",
	}, wrapObserver(s, "agent_save", s.handleSave))

	// agent_forget
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "agent_forget",
		Description: "Delete a finding from persistent memory by key.",
	}, wrapObserver(s, "agent_forget", s.handleForget))

	// agent_session
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "agent_session",
		Description: "Manage chat sessions. Use sessions to organize distinct work streams. Operations: list, create, switch.",
	}, wrapObserver(s, "agent_session", s.handleSession))

	// agent_persona
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "agent_persona",
		Description: "Interact with persona files (SOUL.md, IDENTITY.md, MEMORY.md). Operations: view (all files), read (one file), update (one file, except SOUL.md which is immutable).",
	}, wrapObserver(s, "agent_persona", s.handlePersona))

	// agent_evolve
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "agent_evolve",
		Description: "Manage memory evolution. Operations: status (view stats), run (trigger if thresholds met), force (trigger unconditionally), compact (run full compaction on MEMORY.md).",
	}, wrapObserver(s, "agent_evolve", s.handleEvolve))

	// agent_profile
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "agent_profile",
		Description: "View user behavior profile and system statistics. Operations: view (compact profile), stats (full system statistics).",
	}, wrapObserver(s, "agent_profile", s.handleProfile))

	// agent_health
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "agent_health",
		Description: "Check system health. Returns memory system status, database statistics, and evolution status.",
	}, wrapObserver(s, "agent_health", s.handleHealth))

	// agent_tasks
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "agent_tasks",
		Description: "List, count, or get background daemon tasks. Operations: list, count, get.",
	}, wrapObserver(s, "agent_tasks", s.handleTasks))

	// agent_task_action
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "agent_task_action",
		Description: "Act on a daemon task. Actions: start, complete, dismiss, snooze, comment.",
	}, wrapObserver(s, "agent_task_action", s.handleTaskAction))

	// agent_skill
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "agent_skill",
		Description: "Manage domain skills. Operations: list, view, create, update, evolve, merge.",
	}, wrapObserver(s, "agent_skill", s.handleSkill))
}

// --- Tool Input Structs ---

// RecallInput is the input for agent_recall.
type RecallInput struct {
	Query   string `json:"query" jsonschema:"Search keywords or topics to recall context for"`
	Session string `json:"session,omitempty" jsonschema:"Session name to load history from"`
}

// SaveInput is the input for agent_save.
type SaveInput struct {
	Key        string `json:"key" jsonschema:"Short identifier (e.g. PROJ-1234 or staging-state)"`
	Content    string `json:"content" jsonschema:"Finding content (root cause or state or trace IDs etc.)"`
	Tags       string `json:"tags,omitempty" jsonschema:"Comma-separated tags (e.g. defect,timeout,production)"`
	Importance int    `json:"importance,omitempty" jsonschema:"Levels: 0 is transient, 1 is normal (default), 2 is permanent"`
	Domain     string `json:"domain,omitempty" jsonschema:"Skill domain to route to. Auto-classified if omitted."`
}

// ForgetInput is the input for agent_forget.
type ForgetInput struct {
	Key string `json:"key" jsonschema:"Short identifier of the finding to delete"`
}

// SessionInput is the input for agent_session.
type SessionInput struct {
	Operation string `json:"operation" jsonschema:"Operation: list or create or switch"`
	Name      string `json:"name,omitempty" jsonschema:"Session name (required for create/switch)"`
	ID        string `json:"id,omitempty" jsonschema:"Session ID (alternative to name for switch)"`
}

// PersonaInput is the input for agent_persona.
type PersonaInput struct {
	Operation string `json:"operation" jsonschema:"Operation: view (all), read (one file), or update"`
	File      string `json:"file,omitempty" jsonschema:"Filename: SOUL.md, IDENTITY.md, or MEMORY.md (required for read/update)"`
	Content   string `json:"content,omitempty" jsonschema:"New content (required for update)"`
}

// EvolveInput is the input for agent_evolve.
type EvolveInput struct {
	Operation string `json:"operation" jsonschema:"Operation: status, run, force, or compact"`
}

// ProfileInput is the input for agent_profile.
type ProfileInput struct {
	Operation string `json:"operation" jsonschema:"Operation: view or stats"`
}

// HealthInput is the input for agent_health.
type HealthInput struct {
	Verbose bool `json:"verbose,omitempty" jsonschema:"Include detailed statistics"`
}

// TasksInput is the input for agent_tasks.
type TasksInput struct {
	Operation string `json:"operation" jsonschema:"Operation: list, count, or get"`
	ID        string `json:"id,omitempty" jsonschema:"Task ID (required for get)"`
	Status    string `json:"status,omitempty" jsonschema:"Filter by status: pending, in_progress, completed, dismissed, snoozed"`
	Source    string `json:"source,omitempty" jsonschema:"Filter by source: jira, email, teams, bitbucket"`
	Priority  string `json:"priority,omitempty" jsonschema:"Filter by priority: critical, high, normal, low"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Max results (default 20)"`
}

// TaskActionInput is the input for agent_task_action.
type TaskActionInput struct {
	ID             string `json:"id" jsonschema:"Task ID or source_id"`
	Action         string `json:"action" jsonschema:"Action: start, complete, dismiss, snooze, or comment"`
	Result         string `json:"result,omitempty" jsonschema:"Result text or comment text (for complete/comment)"`
	SnoozeMinutes  int    `json:"snooze_minutes,omitempty" jsonschema:"Minutes to snooze (default 60)"`
}

// SkillInput is the input for agent_skill.
type SkillInput struct {
	Operation   string `json:"operation" jsonschema:"Operation: list, view, create, update, evolve, or merge"`
	Name        string `json:"name,omitempty" jsonschema:"Skill name (for view/create/update/evolve)"`
	Description string `json:"description,omitempty" jsonschema:"Skill description (for create)"`
	Tags        string `json:"tags,omitempty" jsonschema:"Comma-separated tags (for create)"`
	Content     string `json:"content,omitempty" jsonschema:"Full SKILL.md content (for update)"`
	Source      string `json:"source,omitempty" jsonschema:"Source skill name (for merge)"`
	Target      string `json:"target,omitempty" jsonschema:"Target skill name (for merge)"`
}

// --- Tool Handlers ---

func (s *Server) handleRecall(_ context.Context, _ *mcp.CallToolRequest, input RecallInput) (*mcp.CallToolResult, any, error) {
	if input.Query == "" {
		return errorResult("query is required"), nil, nil
	}

	result, err := s.engine.Recall(input.Query, input.Session)
	if err != nil {
		return errorResult(fmt.Sprintf("recall failed: %v", err)), nil, nil
	}

	if result == "" {
		result = "No relevant findings or context found. This appears to be a fresh start."
	}

	return textResult(result), nil, nil
}

func (s *Server) handleSave(_ context.Context, _ *mcp.CallToolRequest, input SaveInput) (*mcp.CallToolResult, any, error) {
	if input.Key == "" {
		return errorResult("key is required"), nil, nil
	}
	if input.Content == "" {
		return errorResult("content is required"), nil, nil
	}

	importance := input.Importance
	if importance < 0 || importance > 2 {
		importance = 1
	}

	result, err := s.engine.Save(input.Key, input.Content, input.Tags, importance, input.Domain)
	if err != nil {
		return errorResult(fmt.Sprintf("save failed: %v", err)), nil, nil
	}

	return textResult(result.Message), nil, nil
}

func (s *Server) handleForget(_ context.Context, _ *mcp.CallToolRequest, input ForgetInput) (*mcp.CallToolResult, any, error) {
	if input.Key == "" {
		return errorResult("key is required"), nil, nil
	}

	result, err := s.engine.Delete(input.Key)
	if err != nil {
		return errorResult(fmt.Sprintf("forget failed: %v", err)), nil, nil
	}

	return textResult(result.Message), nil, nil
}

func (s *Server) handleSession(_ context.Context, _ *mcp.CallToolRequest, input SessionInput) (*mcp.CallToolResult, any, error) {
	switch input.Operation {
	case "list":
		return s.sessionList()
	case "create":
		return s.sessionCreate(input.Name)
	case "switch":
		return s.sessionSwitch(input.Name, input.ID)
	default:
		return errorResult(fmt.Sprintf("unknown operation: %s (use list, create, or switch)", input.Operation)), nil, nil
	}
}

func (s *Server) sessionList() (*mcp.CallToolResult, any, error) {
	sessions, err := s.engine.Sessions.List()
	if err != nil {
		return errorResult(fmt.Sprintf("listing sessions: %v", err)), nil, nil
	}

	if len(sessions) == 0 {
		return textResult("No sessions found."), nil, nil
	}

	currentID := s.engine.Sessions.CurrentID()
	var sb strings.Builder
	sb.WriteString("Sessions:\n")
	for _, sess := range sessions {
		marker := "  "
		if sess.ID == currentID {
			marker = "→ "
		}
		sb.WriteString(fmt.Sprintf("%s%s (id: %s, updated: %s)\n",
			marker, sess.Name, sess.ID, sess.UpdatedAt.Format("2006-01-02 15:04")))
	}
	return textResult(sb.String()), nil, nil
}

func (s *Server) sessionCreate(name string) (*mcp.CallToolResult, any, error) {
	if name == "" {
		return errorResult("name is required for create"), nil, nil
	}

	sess, err := s.engine.Sessions.Create(name)
	if err != nil {
		return errorResult(fmt.Sprintf("creating session: %v", err)), nil, nil
	}

	return textResult(fmt.Sprintf("Created session %q (id: %s)", sess.Name, sess.ID)), nil, nil
}

func (s *Server) sessionSwitch(name, id string) (*mcp.CallToolResult, any, error) {
	target := name
	if target == "" {
		target = id
	}
	if target == "" {
		return errorResult("name or id is required for switch"), nil, nil
	}

	sess, err := s.engine.Sessions.Switch(target)
	if err != nil {
		return errorResult(fmt.Sprintf("switching session: %v", err)), nil, nil
	}

	return textResult(fmt.Sprintf("Switched to session %q (id: %s)", sess.Name, sess.ID)), nil, nil
}

func (s *Server) handlePersona(_ context.Context, _ *mcp.CallToolRequest, input PersonaInput) (*mcp.CallToolResult, any, error) {
	switch input.Operation {
	case "view":
		content, err := s.engine.Persona.ViewAll()
		if err != nil {
			return errorResult(fmt.Sprintf("viewing persona: %v", err)), nil, nil
		}
		return textResult(content), nil, nil

	case "read":
		if input.File == "" {
			return errorResult("file is required for read operation (SOUL.md, IDENTITY.md, or MEMORY.md)"), nil, nil
		}
		content, err := s.engine.Persona.ReadFile(input.File)
		if err != nil {
			return errorResult(fmt.Sprintf("reading %s: %v", input.File, err)), nil, nil
		}
		return textResult(content), nil, nil

	case "update":
		if input.File == "" {
			return errorResult("file is required for update operation"), nil, nil
		}
		if input.Content == "" {
			return errorResult("content is required for update operation"), nil, nil
		}
		err := s.engine.Persona.UpdateFile(input.File, input.Content)
		if err != nil {
			return errorResult(fmt.Sprintf("updating %s: %v", input.File, err)), nil, nil
		}
		return textResult(fmt.Sprintf("Successfully updated %s", input.File)), nil, nil

	default:
		return errorResult(fmt.Sprintf("unknown operation: %s (use view, read, or update)", input.Operation)), nil, nil
	}
}

// --- agent_evolve ---

func (s *Server) handleEvolve(_ context.Context, _ *mcp.CallToolRequest, input EvolveInput) (*mcp.CallToolResult, any, error) {
	switch input.Operation {
	case "status":
		return s.evolveStatus()
	case "run":
		return s.evolveRun(false)
	case "force":
		return s.evolveRun(true)
	case "compact":
		return s.evolveCompact()
	default:
		return errorResult(fmt.Sprintf("unknown operation: %s (use status, run, force, or compact)", input.Operation)), nil, nil
	}
}

func (s *Server) evolveStatus() (*mcp.CallToolResult, any, error) {
	var sb strings.Builder
	sb.WriteString("## Memory System Status\n\n")

	findingsCount, _ := s.engine.Store.CountFindings()
	toolCallCount, _ := s.engine.Store.CountToolCalls()
	evoCount, _ := s.engine.Store.CountEvolutions()
	sessCount, _ := s.engine.Store.CountSessions()

	sb.WriteString(fmt.Sprintf("- Findings: %d\n", findingsCount))
	sb.WriteString(fmt.Sprintf("- Tool Calls: %d\n", toolCallCount))
	sb.WriteString(fmt.Sprintf("- Sessions: %d\n", sessCount))
	sb.WriteString(fmt.Sprintf("- Evolutions: %d\n", evoCount))

	lastEvo, err := s.engine.Store.GetLastEvolutionTime()
	if err == nil && !lastEvo.IsZero() {
		sb.WriteString(fmt.Sprintf("- Last Evolution: %s\n", lastEvo.Format("2006-01-02 15:04:05")))
	} else {
		sb.WriteString("- Last Evolution: never\n")
	}

	// Recent evolutions
	evos, _ := s.engine.Store.GetRecentEvolutions(5)
	if len(evos) > 0 {
		sb.WriteString("\n### Recent Evolution History\n\n")
		for _, e := range evos {
			sb.WriteString(fmt.Sprintf("- [%s] %s → %s (findings: %d)\n",
				e.CreatedAt.Format("2006-01-02 15:04"),
				e.EvolutionType, e.TargetFile, e.FindingsCount))
		}
	}

	return textResult(sb.String()), nil, nil
}

func (s *Server) evolveRun(force bool) (*mcp.CallToolResult, any, error) {
	if s.engine.EvoEngine == nil {
		return errorResult("evolution engine is not initialized"), nil, nil
	}

	if err := s.engine.EvoEngine.Run(); err != nil {
		return errorResult(fmt.Sprintf("evolution failed: %v", err)), nil, nil
	}
	if force {
		return textResult("Forced evolution completed successfully."), nil, nil
	}
	return textResult("Evolution completed successfully."), nil, nil
}

func (s *Server) evolveCompact() (*mcp.CallToolResult, any, error) {
	if s.engine.EvoEngine == nil {
		return errorResult("evolution engine is not initialized"), nil, nil
	}

	if err := s.engine.EvoEngine.RunCompaction(); err != nil {
		return errorResult(fmt.Sprintf("compaction failed: %v", err)), nil, nil
	}
	return textResult("Full compaction of MEMORY.md completed successfully."), nil, nil
}

// --- agent_profile ---

func (s *Server) handleProfile(_ context.Context, _ *mcp.CallToolRequest, input ProfileInput) (*mcp.CallToolResult, any, error) {
	switch input.Operation {
	case "view":
		if s.engine.Profiler == nil {
			return textResult("Profile tracking is not enabled."), nil, nil
		}
		profile := s.engine.Profiler.CompactProfile(0)
		if profile == "" {
			return textResult("No profile data collected yet."), nil, nil
		}
		return textResult(profile), nil, nil

	case "stats":
		return s.systemStats()

	default:
		return errorResult(fmt.Sprintf("unknown operation: %s (use view or stats)", input.Operation)), nil, nil
	}
}

func (s *Server) systemStats() (*mcp.CallToolResult, any, error) {
	var sb strings.Builder
	sb.WriteString("## System Statistics\n\n")

	findingsCount, _ := s.engine.Store.CountFindings()
	toolCallCount, _ := s.engine.Store.CountToolCalls()
	evoCount, _ := s.engine.Store.CountEvolutions()
	sessCount, _ := s.engine.Store.CountSessions()

	sb.WriteString(fmt.Sprintf("- Total Findings: %d\n", findingsCount))
	sb.WriteString(fmt.Sprintf("- Total Tool Calls: %d\n", toolCallCount))
	sb.WriteString(fmt.Sprintf("- Total Sessions: %d\n", sessCount))
	sb.WriteString(fmt.Sprintf("- Total Evolutions: %d\n", evoCount))

	// Top tools
	topTools, _ := s.engine.Store.GetTopToolCalls(10)
	if len(topTools) > 0 {
		sb.WriteString("\n### Top Tools\n")
		for _, t := range topTools {
			sb.WriteString(fmt.Sprintf("- %s (%d calls)\n", t.ToolName, t.Count))
		}
	}

	// Top environments from profile
	if s.engine.Profiler != nil {
		envs, _ := s.engine.Store.GetProfileByCategory("environment", 5)
		if len(envs) > 0 {
			sb.WriteString("\n### Top Environments\n")
			for _, e := range envs {
				sb.WriteString(fmt.Sprintf("- %s (%d hits)\n", e.Value, e.HitCount))
			}
		}
	}

	return textResult(sb.String()), nil, nil
}

// --- agent_health ---

func (s *Server) handleHealth(_ context.Context, _ *mcp.CallToolRequest, input HealthInput) (*mcp.CallToolResult, any, error) {
	var sb strings.Builder
	sb.WriteString("## Health Report\n\n")

	// Memory system
	sb.WriteString("### Memory System: OK\n")
	findingsCount, err := s.engine.Store.CountFindings()
	if err != nil {
		sb.WriteString(fmt.Sprintf("- Database: ERROR (%v)\n", err))
	} else {
		sb.WriteString(fmt.Sprintf("- Database: OK\n"))
		sb.WriteString(fmt.Sprintf("- Findings: %d\n", findingsCount))
	}

	toolCallCount, _ := s.engine.Store.CountToolCalls()
	sb.WriteString(fmt.Sprintf("- Tool Calls: %d\n", toolCallCount))

	if size, err := s.engine.Store.GetDBSize(); err == nil {
		mb := float64(size) / (1024 * 1024)
		sb.WriteString(fmt.Sprintf("- DB Size: %.2f MB (%d bytes)\n", mb, size))
	}

	// Evolution
	evoEnabled := s.engine.Cfg.Evolution.Enabled
	sb.WriteString(fmt.Sprintf("\n### Evolution: %s\n", boolStatus(evoEnabled)))
	if evoEnabled {
		lastEvo, err := s.engine.Store.GetLastEvolutionTime()
		if err == nil && !lastEvo.IsZero() {
			sb.WriteString(fmt.Sprintf("- Last Run: %s\n", lastEvo.Format("2006-01-02 15:04:05")))
		} else {
			sb.WriteString("- Last Run: never\n")
		}
	}

	// Observation
	sb.WriteString(fmt.Sprintf("\n### Observation: %s\n", boolStatus(s.engine.Cfg.Observation.Enabled)))

	// Profile
	sb.WriteString(fmt.Sprintf("\n### Profile Tracking: %s\n", boolStatus(s.engine.Cfg.Profile.Enabled)))

	// Skills
	if s.engine.Skills != nil {
		skills, err := s.engine.Skills.LoadAll()
		if err == nil {
			sb.WriteString(fmt.Sprintf("\n### Skills: %d loaded\n", len(skills)))
			if input.Verbose {
				for _, sk := range skills {
					sb.WriteString(fmt.Sprintf("- %s: %s (auto_evolve: %v)\n", sk.Name, sk.Description, sk.IsAutoEvolve()))
				}
			}
		}
	}

	return textResult(sb.String()), nil, nil
}

func boolStatus(b bool) string {
	if b {
		return "ENABLED"
	}
	return "DISABLED"
}

// --- agent_tasks ---

func (s *Server) handleTasks(_ context.Context, _ *mcp.CallToolRequest, input TasksInput) (*mcp.CallToolResult, any, error) {
	switch input.Operation {
	case "list":
		return s.tasksList(input)
	case "count":
		return s.tasksCount()
	case "get":
		return s.tasksGet(input.ID)
	default:
		return errorResult(fmt.Sprintf("unknown operation: %s (use list, count, or get)", input.Operation)), nil, nil
	}
}

func (s *Server) tasksList(input TasksInput) (*mcp.CallToolResult, any, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}

	tasks, err := s.engine.Store.GetPendingDaemonTasks(limit)
	if err != nil {
		return errorResult(fmt.Sprintf("listing tasks: %v", err)), nil, nil
	}

	if len(tasks) == 0 {
		return textResult("No pending tasks."), nil, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Pending Tasks (%d)\n\n", len(tasks)))
	for _, t := range tasks {
		sb.WriteString(fmt.Sprintf("- [%s] **%s**: %s (source: %s, id: %s)\n",
			t.Priority, t.Title, t.Summary, t.Source, t.ID[:8]))
	}

	return textResult(sb.String()), nil, nil
}

func (s *Server) tasksCount() (*mcp.CallToolResult, any, error) {
	counts, err := s.engine.Store.CountDaemonTasksByStatus()
	if err != nil {
		return errorResult(fmt.Sprintf("counting tasks: %v", err)), nil, nil
	}

	if len(counts) == 0 {
		return textResult("No tasks found."), nil, nil
	}

	var sb strings.Builder
	sb.WriteString("## Task Counts\n\n")
	total := 0
	for status, count := range counts {
		sb.WriteString(fmt.Sprintf("- %s: %d\n", status, count))
		total += count
	}
	sb.WriteString(fmt.Sprintf("- **total**: %d\n", total))

	return textResult(sb.String()), nil, nil
}

func (s *Server) tasksGet(id string) (*mcp.CallToolResult, any, error) {
	if id == "" {
		return errorResult("id is required for get operation"), nil, nil
	}

	// Try by ID first, then by source_id
	task, err := s.engine.Store.GetDaemonTaskByID(id)
	if err != nil {
		task, err = s.engine.Store.GetDaemonTaskBySourceID(id)
		if err != nil {
			return errorResult(fmt.Sprintf("task not found: %s", id)), nil, nil
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Task: %s\n\n", task.Title))
	sb.WriteString(fmt.Sprintf("- **ID**: %s\n", task.ID))
	sb.WriteString(fmt.Sprintf("- **Source**: %s (%s)\n", task.Source, task.SourceID))
	sb.WriteString(fmt.Sprintf("- **Status**: %s\n", task.Status))
	sb.WriteString(fmt.Sprintf("- **Priority**: %s\n", task.Priority))
	sb.WriteString(fmt.Sprintf("- **Category**: %s\n", task.Category))
	sb.WriteString(fmt.Sprintf("- **Created**: %s\n", task.CreatedAt.Format("2006-01-02 15:04")))
	sb.WriteString(fmt.Sprintf("\n### Summary\n\n%s\n", task.Summary))
	if task.ActionResult != "" {
		sb.WriteString(fmt.Sprintf("\n### Action Result\n\n%s\n", task.ActionResult))
	}

	return textResult(sb.String()), nil, nil
}

// --- agent_task_action ---

func (s *Server) handleTaskAction(_ context.Context, _ *mcp.CallToolRequest, input TaskActionInput) (*mcp.CallToolResult, any, error) {
	if input.ID == "" {
		return errorResult("id is required"), nil, nil
	}
	if input.Action == "" {
		return errorResult("action is required"), nil, nil
	}

	// resolve task: try by ID, then by source_id
	task, err := s.engine.Store.GetDaemonTaskByID(input.ID)
	if err != nil {
		task, err = s.engine.Store.GetDaemonTaskBySourceID(input.ID)
		if err != nil {
			return errorResult(fmt.Sprintf("task not found: %s", input.ID)), nil, nil
		}
	}

	switch input.Action {
	case "start":
		err = s.engine.Store.UpdateDaemonTaskStatus(task.ID, "in_progress", "")
	case "complete":
		err = s.engine.Store.UpdateDaemonTaskStatus(task.ID, "completed", input.Result)
	case "dismiss":
		err = s.engine.Store.UpdateDaemonTaskStatus(task.ID, "dismissed", input.Result)
	case "snooze":
		minutes := input.SnoozeMinutes
		if minutes <= 0 {
			minutes = 60
		}
		until := time.Now().Add(time.Duration(minutes) * time.Minute)
		err = s.engine.Store.SnoozeDaemonTask(task.ID, until)
	case "comment":
		// Append comment to action_result
		existing := task.ActionResult
		if existing != "" {
			existing += "\n"
		}
		existing += fmt.Sprintf("[%s] %s", time.Now().Format("2006-01-02 15:04"), input.Result)
		err = s.engine.Store.UpdateDaemonTaskStatus(task.ID, task.Status, existing)
	default:
		return errorResult(fmt.Sprintf("unknown action: %s (use start, complete, dismiss, snooze, or comment)", input.Action)), nil, nil
	}

	if err != nil {
		return errorResult(fmt.Sprintf("action failed: %v", err)), nil, nil
	}

	return textResult(fmt.Sprintf("Action %q applied to task %s (%s)", input.Action, task.Title, task.ID[:8])), nil, nil
}

// --- agent_skill ---

func (s *Server) handleSkill(_ context.Context, _ *mcp.CallToolRequest, input SkillInput) (*mcp.CallToolResult, any, error) {
	switch input.Operation {
	case "list":
		return s.skillList()
	case "view":
		return s.skillView(input.Name)
	case "create":
		return s.skillCreate(input.Name, input.Description, input.Tags)
	case "update":
		return s.skillUpdate(input.Name, input.Content)
	case "evolve":
		return s.skillEvolve(input.Name)
	case "merge":
		return s.skillMerge(input.Source, input.Target)
	default:
		return errorResult(fmt.Sprintf("unknown operation: %s (use list, view, create, update, evolve, or merge)", input.Operation)), nil, nil
	}
}

func (s *Server) skillList() (*mcp.CallToolResult, any, error) {
	allSkills, err := s.engine.Skills.LoadAll()
	if err != nil {
		return errorResult(fmt.Sprintf("loading skills: %v", err)), nil, nil
	}
	if len(allSkills) == 0 {
		return textResult("No skills found. Create one with agent_skill(operation=\"create\", name=\"...\", description=\"...\", tags=\"...\")"), nil, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Skills (%d)\n\n", len(allSkills)))
	for _, sk := range allSkills {
		autoEvolve := "yes"
		if !sk.IsAutoEvolve() {
			autoEvolve = "no"
		}
		sb.WriteString(fmt.Sprintf("- **%s**: %s (tags: %s, auto_evolve: %s)\n",
			sk.Name, sk.Description, sk.Metadata.Tags, autoEvolve))
	}

	return textResult(sb.String()), nil, nil
}

func (s *Server) skillView(name string) (*mcp.CallToolResult, any, error) {
	if name == "" {
		return errorResult("name is required for view"), nil, nil
	}

	allSkills, err := s.engine.Skills.LoadAll()
	if err != nil {
		return errorResult(fmt.Sprintf("loading skills: %v", err)), nil, nil
	}

	for _, sk := range allSkills {
		if strings.EqualFold(sk.Name, name) {
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("## Skill: %s\n\n", sk.Name))
			sb.WriteString(fmt.Sprintf("**Description**: %s\n", sk.Description))
			sb.WriteString(fmt.Sprintf("**Tags**: %s\n", sk.Metadata.Tags))
			sb.WriteString(fmt.Sprintf("**Tool Prefixes**: %s\n", sk.Metadata.ToolPrefixes))
			sb.WriteString(fmt.Sprintf("**Auto-evolve**: %s\n", sk.Metadata.AutoEvolve))
			sb.WriteString(fmt.Sprintf("\n---\n\n%s\n", sk.Markdown))
			return textResult(sb.String()), nil, nil
		}
	}

	return errorResult(fmt.Sprintf("skill %q not found", name)), nil, nil
}

func (s *Server) skillCreate(name, description, tags string) (*mcp.CallToolResult, any, error) {
	if name == "" {
		return errorResult("name is required for create"), nil, nil
	}

	err := s.engine.Skills.Create(name, description, tags)
	if err != nil {
		return errorResult(fmt.Sprintf("creating skill: %v", err)), nil, nil
	}

	return textResult(fmt.Sprintf("Created skill %q", name)), nil, nil
}

func (s *Server) skillUpdate(name, content string) (*mcp.CallToolResult, any, error) {
	if name == "" {
		return errorResult("name is required for update"), nil, nil
	}
	if content == "" {
		return errorResult("content is required for update"), nil, nil
	}

	allSkills, err := s.engine.Skills.LoadAll()
	if err != nil {
		return errorResult(fmt.Sprintf("loading skills: %v", err)), nil, nil
	}

	for _, sk := range allSkills {
		if strings.EqualFold(sk.Name, name) {
			if err := sk.UpdateMarkdown(content); err != nil {
				return errorResult(fmt.Sprintf("updating skill: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Updated skill %q", name)), nil, nil
		}
	}

	return errorResult(fmt.Sprintf("skill %q not found", name)), nil, nil
}

func (s *Server) skillEvolve(name string) (*mcp.CallToolResult, any, error) {
	if name == "" {
		return errorResult("name is required for evolve"), nil, nil
	}
	if s.engine.SkillEvolver == nil {
		return errorResult("skill evolution is not initialized"), nil, nil
	}

	allSkills, err := s.engine.Skills.LoadAll()
	if err != nil {
		return errorResult(fmt.Sprintf("loading skills: %v", err)), nil, nil
	}

	for _, sk := range allSkills {
		if strings.EqualFold(sk.Name, name) {
			if err := s.engine.SkillEvolver.EvolveSkill(sk); err != nil {
				return errorResult(fmt.Sprintf("evolving skill: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Evolved skill %q with domain-specific patterns", name)), nil, nil
		}
	}

	return errorResult(fmt.Sprintf("skill %q not found", name)), nil, nil
}

func (s *Server) skillMerge(source, target string) (*mcp.CallToolResult, any, error) {
	if source == "" || target == "" {
		return errorResult("both source and target are required for merge"), nil, nil
	}

	allSkills, err := s.engine.Skills.LoadAll()
	if err != nil {
		return errorResult(fmt.Sprintf("loading skills: %v", err)), nil, nil
	}

	var srcSkill, tgtSkill *skills.Skill
	for _, sk := range allSkills {
		if strings.EqualFold(sk.Name, source) {
			srcSkill = sk
		}
		if strings.EqualFold(sk.Name, target) {
			tgtSkill = sk
		}
	}

	if srcSkill == nil {
		return errorResult(fmt.Sprintf("source skill %q not found", source)), nil, nil
	}
	if tgtSkill == nil {
		return errorResult(fmt.Sprintf("target skill %q not found", target)), nil, nil
	}

	// Merge: append source's learned patterns to target, deduplicating
	err = s.engine.Skills.MergeSkills(srcSkill, tgtSkill)
	if err != nil {
		return errorResult(fmt.Sprintf("merging skills: %v", err)), nil, nil
	}

	return textResult(fmt.Sprintf("Merged patterns from %q into %q", source, target)), nil, nil
}

