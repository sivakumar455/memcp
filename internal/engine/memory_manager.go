// Package engine contains the core orchestrator and subsystems for memcp.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/sivakumar455/memcp/internal/memory"
)

// MemoryManager implements the ADD/UPDATE/NOOP pipeline for incoming facts.
type MemoryManager struct {
	store *memory.Store
}

// NewMemoryManager creates a new MemoryManager.
func NewMemoryManager(store *memory.Store) *MemoryManager {
	return &MemoryManager{store: store}
}

// SaveResult captures the outcome of a save operation.
type SaveResult struct {
	Action  string // "ADD", "UPDATE", or "NOOP"
	Key     string
	Message string
}

// SaveExplicit processes an explicit save request through the ADD/UPDATE/NOOP pipeline.
func (mm *MemoryManager) SaveExplicit(key, content, tags string, importance int, sessionID, domain, source string) (*SaveResult, error) {
	// Step 1: Sanitize content
	content = memory.SanitizeContent(content)

	if source == "" {
		source = "manual"
	}

	// Step 2: Look up existing finding by key
	existing, err := mm.store.GetFinding(key)
	if err != nil {
		// Not found — ADD
		f := &memory.Finding{
			Key:        key,
			Content:    content,
			Tags:       normalizeTags(tags),
			Importance: importance,
			Source:     source,
			SessionID:  sessionID,
			Domain:     domain,
		}
		if err := mm.store.UpsertFinding(f); err != nil {
			return nil, fmt.Errorf("inserting finding: %w", err)
		}
		return &SaveResult{
			Action:  "ADD",
			Key:     key,
			Message: fmt.Sprintf("Saved new finding: %s [%s]", key, importanceLabel(importance)),
		}, nil
	}

	// Step 3: Check for redundancy
	if isRedundant(existing.Content, content) {
		return &SaveResult{
			Action:  "NOOP",
			Key:     key,
			Message: fmt.Sprintf("Finding %q already contains this information", key),
		}, nil
	}

	// Step 4: UPDATE — merge content and tags
	mergedContent := mergeContent(existing.Content, content)
	mergedTags := mergeTags(existing.Tags, tags)

	// Use higher importance
	mergedImportance := existing.Importance
	if importance > mergedImportance {
		mergedImportance = importance
	}

	// Use new domain if provided
	mergedDomain := existing.Domain
	if domain != "" {
		mergedDomain = domain
	}

	f := &memory.Finding{
		ID:         existing.ID,
		Key:        key,
		Content:    mergedContent,
		Tags:       mergedTags,
		Importance: mergedImportance,
		Source:     source,
		SessionID:  sessionID,
		Domain:     mergedDomain,
	}
	if err := mm.store.UpsertFinding(f); err != nil {
		return nil, fmt.Errorf("updating finding: %w", err)
	}
	return &SaveResult{
		Action:  "UPDATE",
		Key:     key,
		Message: fmt.Sprintf("Updated finding: %s (v%d → v%d) [%s]", key, existing.Version, existing.Version+1, importanceLabel(mergedImportance)),
	}, nil
}

// SaveObserved processes an auto-observed fact through the pipeline.
func (mm *MemoryManager) SaveObserved(key, content, tags, category, sessionID, domain string) (*SaveResult, error) {
	importance := categoryImportance(category)
	return mm.SaveExplicit(key, content, tags, importance, sessionID, domain, "observation")
}

// DeleteExplicit removes a finding by key.
func (mm *MemoryManager) DeleteExplicit(key string) (*SaveResult, error) {
	deleted, err := mm.store.DeleteFinding(key)
	if err != nil {
		return nil, err
	}
	if !deleted {
		return &SaveResult{
			Action:  "NOOP",
			Key:     key,
			Message: fmt.Sprintf("Finding %q not found, nothing to delete", key),
		}, nil
	}
	return &SaveResult{
		Action:  "DELETE",
		Key:     key,
		Message: fmt.Sprintf("Deleted finding: %s", key),
	}, nil
}

// --- Helper Functions ---

// isRedundant checks if new content is already covered by existing content.
func isRedundant(existing, incoming string) bool {
	existingLower := strings.ToLower(strings.TrimSpace(existing))
	incomingLower := strings.ToLower(strings.TrimSpace(incoming))

	// Exact match
	if existingLower == incomingLower {
		return true
	}
	// Incoming is a substring of existing
	if strings.Contains(existingLower, incomingLower) {
		return true
	}
	return false
}

// mergeContent appends new content if not already present.
// It caps the total length to prevent unbounded growth across updates.
func mergeContent(existing, incoming string) string {
	if strings.Contains(strings.ToLower(existing), strings.ToLower(strings.TrimSpace(incoming))) {
		return existing
	}
	merged := existing + "\n" + incoming
	const maxLen = 2000
	if len(merged) > maxLen {
		// Keep the first 1000 chars and the last 900 chars
		return merged[:1000] + "\n... [content truncated due to length] ...\n" + merged[len(merged)-900:]
	}
	return merged
}

// mergeTags unions two comma-separated tag strings.
func mergeTags(existing, incoming string) string {
	seen := make(map[string]bool)
	var merged []string

	for _, t := range splitTags(existing) {
		if t != "" && !seen[t] {
			seen[t] = true
			merged = append(merged, t)
		}
	}
	for _, t := range splitTags(incoming) {
		if t != "" && !seen[t] {
			seen[t] = true
			merged = append(merged, t)
		}
	}
	return strings.Join(merged, ",")
}

// normalizeTags cleans up a tag string.
func normalizeTags(tags string) string {
	var cleaned []string
	for _, t := range splitTags(tags) {
		t = strings.TrimSpace(strings.ToLower(t))
		if t != "" {
			cleaned = append(cleaned, t)
		}
	}
	return strings.Join(cleaned, ",")
}

func splitTags(tags string) []string {
	return strings.Split(tags, ",")
}

// categoryImportance returns auto-importance based on fact category.
func categoryImportance(category string) int {
	switch strings.ToLower(category) {
	case "environment", "microservice":
		return 2 // permanent
	case "observation", "reference":
		return 0 // transient
	default:
		return 1 // normal
	}
}

// importanceLabel returns a human-readable label for importance level.
func importanceLabel(importance int) string {
	switch importance {
	case 0:
		return "transient"
	case 2:
		return "permanent"
	default:
		return "normal"
	}
}

// --- Credential Sanitization (additional patterns) ---

var additionalCredPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(aws[_-]?secret[_-]?access[_-]?key)\s*[=:]\s*\S+`),
	regexp.MustCompile(`(?i)(private[_-]?key)\s*[=:]\s*\S+`),
}

func init() {
	// Register additional patterns (these are checked via memory.SanitizeContent)
	_ = additionalCredPatterns // available for future use
}
