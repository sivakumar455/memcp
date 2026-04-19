// Package persona handles loading and updating the three-file soul system
// (SOUL.md, IDENTITY.md, MEMORY.md) for memcp.
package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Files in the soul directory, in load order.
const (
	FileSoul     = "SOUL.md"
	FileIdentity = "IDENTITY.md"
	FileMemory   = "MEMORY.md"
)

// Loader manages persona file loading and updates.
type Loader struct {
	soulDir         string
	maxCharsPerFile int
	totalMaxChars   int
}

// NewLoader creates a persona loader.
func NewLoader(soulDir string, maxCharsPerFile, totalMaxChars int) *Loader {
	if maxCharsPerFile <= 0 {
		maxCharsPerFile = 20000
	}
	if totalMaxChars <= 0 {
		totalMaxChars = 100000
	}
	return &Loader{
		soulDir:         soulDir,
		maxCharsPerFile: maxCharsPerFile,
		totalMaxChars:   totalMaxChars,
	}
}

// SoulDir returns the configured soul directory path.
func (l *Loader) SoulDir() string {
	return l.soulDir
}

// PersonaFile represents a loaded persona file's content.
type PersonaFile struct {
	Name    string // e.g. "SOUL.md"
	Content string
	Path    string
}

// LoadAll loads all three persona files in order: SOUL.md, IDENTITY.md, MEMORY.md.
// Applies per-file and total character limits with section-aware truncation.
func (l *Loader) LoadAll() ([]*PersonaFile, error) {
	files := []string{FileSoul, FileIdentity, FileMemory}
	var loaded []*PersonaFile
	totalChars := 0

	for _, name := range files {
		// Check if we've exhausted the total budget
		remaining := l.totalMaxChars - totalChars
		if remaining <= 0 {
			break
		}

		pf, err := l.loadFile(name, remaining)
		if err != nil {
			// File not found is not fatal — just skip it
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("loading %s: %w", name, err)
		}

		loaded = append(loaded, pf)
		totalChars += len(pf.Content)
	}

	return loaded, nil
}

// LoadForContext loads SOUL.md and IDENTITY.md for Tier 0 context injection.
// MEMORY.md is excluded (it's covered by Tier 2 via FTS5 search).
func (l *Loader) LoadForContext() (string, error) {
	files := []string{FileSoul, FileIdentity}
	var sections []string
	totalChars := 0

	for _, name := range files {
		remaining := l.totalMaxChars - totalChars
		if remaining <= 0 {
			break
		}

		pf, err := l.loadFile(name, remaining)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("loading %s: %w", name, err)
		}

		sections = append(sections, fmt.Sprintf("--- %s ---\n%s", name, pf.Content))
		totalChars += len(pf.Content)
	}

	return strings.Join(sections, "\n\n"), nil
}

// ReadFile reads a single persona file by name.
func (l *Loader) ReadFile(name string) (string, error) {
	if !isValidPersonaFile(name) {
		return "", fmt.Errorf("invalid persona file: %s (must be SOUL.md, IDENTITY.md, or MEMORY.md)", name)
	}

	path := filepath.Join(l.soulDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// UpdateFile writes new content to a persona file.
// SOUL.md is immutable and cannot be updated through this method.
func (l *Loader) UpdateFile(name, content string) error {
	if !isValidPersonaFile(name) {
		return fmt.Errorf("invalid persona file: %s (must be SOUL.md, IDENTITY.md, or MEMORY.md)", name)
	}

	// SOUL.md is immutable
	if name == FileSoul {
		return fmt.Errorf("SOUL.md is immutable and cannot be modified by the system. Only the user can edit it directly")
	}

	// Ensure directory exists
	if err := os.MkdirAll(l.soulDir, 0755); err != nil {
		return fmt.Errorf("creating soul directory: %w", err)
	}

	path := filepath.Join(l.soulDir, name)
	return os.WriteFile(path, []byte(content), 0644)
}

// ViewAll returns all persona files concatenated with section headers.
func (l *Loader) ViewAll() (string, error) {
	files, err := l.LoadAll()
	if err != nil {
		return "", err
	}

	if len(files) == 0 {
		return "No persona files found. Create soul/SOUL.md, soul/IDENTITY.md, and soul/MEMORY.md to get started.", nil
	}

	var sections []string
	for _, pf := range files {
		sections = append(sections, fmt.Sprintf("--- %s ---\n%s", pf.Name, pf.Content))
	}
	return strings.Join(sections, "\n\n"), nil
}

// --- Internal ---

// loadFile reads and truncates a persona file respecting the character budget.
func (l *Loader) loadFile(name string, remainingBudget int) (*PersonaFile, error) {
	path := filepath.Join(l.soulDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := string(data)

	// Apply per-file limit
	maxChars := l.maxCharsPerFile
	if remainingBudget < maxChars {
		maxChars = remainingBudget
	}

	if len(content) > maxChars {
		content = truncateAtSectionBoundary(content, maxChars)
	}

	return &PersonaFile{
		Name:    name,
		Content: content,
		Path:    path,
	}, nil
}

// truncateAtSectionBoundary truncates content at the nearest ## section boundary
// that keeps the content within maxLen characters.
func truncateAtSectionBoundary(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}

	// Find all section boundaries (lines starting with "## ")
	lines := strings.Split(content, "\n")
	var sectionStarts []int // character offset of each section start
	charOffset := 0

	for i, line := range lines {
		if strings.HasPrefix(line, "## ") && i > 0 {
			sectionStarts = append(sectionStarts, charOffset)
		}
		charOffset += len(line) + 1 // +1 for newline
	}

	// Find the last section boundary that fits within maxLen
	bestCut := maxLen
	for _, offset := range sectionStarts {
		if offset <= maxLen {
			bestCut = offset
		} else {
			break
		}
	}

	truncated := content[:bestCut]

	// Clean up: remove trailing whitespace/newlines
	truncated = strings.TrimRight(truncated, "\n\r\t ")

	// Add truncation notice
	truncated += "\n\n*[Content truncated due to character limit]*"

	return truncated
}

// isValidPersonaFile checks if a filename is one of the three persona files.
func isValidPersonaFile(name string) bool {
	switch name {
	case FileSoul, FileIdentity, FileMemory:
		return true
	default:
		return false
	}
}
