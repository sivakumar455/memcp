package engine

import (
	"fmt"
	"strings"

	"github.com/sivakumar455/memcp/internal/config"
	"github.com/sivakumar455/memcp/internal/memory"
)

// ContextBuilder assembles tiered context for agent_recall.
type ContextBuilder struct {
	store  *memory.Store
	cfg    config.ContextConfig
	maxChars int
}

// NewContextBuilder creates a new ContextBuilder.
func NewContextBuilder(store *memory.Store, cfg config.ContextConfig) *ContextBuilder {
	maxChars := cfg.MaxChars
	if maxChars <= 0 {
		maxChars = 80000
	}
	return &ContextBuilder{
		store:    store,
		cfg:      cfg,
		maxChars: maxChars,
	}
}

// BuildOptions controls what gets included in the context.
type BuildOptions struct {
	Query         string
	SessionID     string
	PersonaText   string // Tier 0: injected persona (SOUL.md + IDENTITY.md)
	SkillsText    string // Tier 0: matching skills
	ProfileText   string // Tier 0: compact user profile
	PendingTasks  string // Bonus: daemon task summary
	ContextWindow int    // Max messages to load for history
}

// Build assembles the full tiered context string.
func (cb *ContextBuilder) Build(opts BuildOptions) (string, error) {
	var sections []string

	coreBudget := cb.maxChars * cb.cfg.CoreBudgetPct / 100
	workBudget := cb.maxChars * cb.cfg.WorkBudgetPct / 100
	relevBudget := cb.maxChars * cb.cfg.RelevBudgetPct / 100
	histBudget := cb.maxChars * cb.cfg.HistBudgetPct / 100

	// --- Tier 0: Core (Persona + Skills + Profile) ---
	tier0 := cb.buildTier0(opts, coreBudget)
	if tier0 != "" {
		sections = append(sections, tier0)
	}

	// --- Tier 1: Working Memory (Recent tool calls) ---
	tier1, err := cb.buildTier1(opts.SessionID, workBudget)
	if err != nil {
		return "", fmt.Errorf("building tier 1: %w", err)
	}
	if tier1 != "" {
		sections = append(sections, tier1)
	}

	// --- Tier 2: Relevant Findings (FTS5 search) ---
	tier2, err := cb.buildTier2(opts.Query, relevBudget)
	if err != nil {
		return "", fmt.Errorf("building tier 2: %w", err)
	}
	if tier2 != "" {
		sections = append(sections, tier2)
	}

	// --- Tier 3: Conversation History ---
	tier3, err := cb.buildTier3(opts.SessionID, opts.ContextWindow, histBudget)
	if err != nil {
		return "", fmt.Errorf("building tier 3: %w", err)
	}
	if tier3 != "" {
		sections = append(sections, tier3)
	}

	// --- Bonus: Pending Tasks ---
	if opts.PendingTasks != "" {
		sections = append(sections, opts.PendingTasks)
	}

	return strings.Join(sections, "\n\n"), nil
}

// buildTier0 assembles core context (persona + skills + profile).
func (cb *ContextBuilder) buildTier0(opts BuildOptions, budget int) string {
	var parts []string
	remaining := budget

	// Persona text (SOUL.md + IDENTITY.md)
	if opts.PersonaText != "" {
		text := truncate(opts.PersonaText, remaining)
		parts = append(parts, text)
		remaining -= len(text)
	}

	// Skills text
	if opts.SkillsText != "" && remaining > 0 {
		text := truncate(opts.SkillsText, remaining)
		parts = append(parts, text)
		remaining -= len(text)
	}

	// Profile text
	if opts.ProfileText != "" && remaining > 0 {
		text := truncate(opts.ProfileText, remaining)
		parts = append(parts, text)
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// buildTier1 assembles working memory from recent tool calls.
func (cb *ContextBuilder) buildTier1(sessionID string, budget int) (string, error) {
	calls, err := cb.store.GetRecentToolCalls(sessionID, 30)
	if err != nil {
		return "", err
	}
	if len(calls) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString("--- Working Memory (Recent Tool Calls) ---\n")

	for _, tc := range calls {
		line := fmt.Sprintf("- %s(%s): %s", tc.ToolName, tc.ArgsSummary, tc.ResultSummary)
		if tc.ElapsedMs > 0 {
			line += fmt.Sprintf(" [%dms]", tc.ElapsedMs)
		}
		line += "\n"

		if sb.Len()+len(line) > budget {
			break
		}
		sb.WriteString(line)
	}

	return sb.String(), nil
}

// buildTier2 assembles relevant findings via FTS5 search.
func (cb *ContextBuilder) buildTier2(query string, budget int) (string, error) {
	if query == "" {
		return "", nil
	}

	findings, err := cb.store.SearchFindings(query, 50)
	if err != nil {
		return "", err
	}
	if len(findings) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString("--- Relevant Findings ---\n")

	for _, f := range findings {
		// Full-length entry
		entry := formatFinding(f)

		if sb.Len()+len(entry) > budget {
			// Try compressed version
			compressed := formatFindingCompressed(f)
			if sb.Len()+len(compressed) > budget {
				break
			}
			sb.WriteString(compressed)
			continue
		}
		sb.WriteString(entry)
	}

	return sb.String(), nil
}

// buildTier3 assembles conversation history.
func (cb *ContextBuilder) buildTier3(sessionID string, contextWindow, budget int) (string, error) {
	if sessionID == "" {
		return "", nil
	}
	if contextWindow <= 0 {
		contextWindow = 50
	}

	msgs, err := cb.store.GetMessages(sessionID, contextWindow)
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString("--- Recent Conversation ---\n")

	for _, m := range msgs {
		line := fmt.Sprintf("[%s] %s\n", m.Role, m.Content)
		if sb.Len()+len(line) > budget {
			break
		}
		sb.WriteString(line)
	}

	return sb.String(), nil
}

// --- Formatting Helpers ---

func formatFinding(f *memory.Finding) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**%s** (%s) [%s]",
		f.Key,
		f.UpdatedAt.Format("2006-01-02 15:04"),
		importanceLabel(f.Importance),
	))
	if f.Tags != "" {
		sb.WriteString(fmt.Sprintf(" {%s}", f.Tags))
	}
	sb.WriteString("\n")
	sb.WriteString(f.Content)
	sb.WriteString("\n\n")
	return sb.String()
}

func formatFindingCompressed(f *memory.Finding) string {
	content := f.Content
	if len(content) > 200 {
		content = content[:200] + "..."
	}
	return fmt.Sprintf("- **%s** [%s]: %s\n", f.Key, importanceLabel(f.Importance), content)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
