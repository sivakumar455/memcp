// Package evolution implements the background evolution engine and progressive
// summarization compactor for memcp.
package evolution

import (
	"fmt"
	"strings"
	"time"

	"github.com/sivakumar455/memcp/internal/memory"
	"github.com/sivakumar455/memcp/internal/persona"
)

// Compactor organizes findings into MEMORY.md progressively.
type Compactor struct {
	store   *memory.Store
	persona *persona.Loader
}

// NewCompactor creates a new Compactor.
func NewCompactor(store *memory.Store, persona *persona.Loader) *Compactor {
	return &Compactor{
		store:   store,
		persona: persona,
	}
}

// AppendToMemory appends a new finding to the Active Findings section of MEMORY.md.
func (c *Compactor) AppendToMemory(f *memory.Finding) error {
	content, err := c.persona.ReadFile(persona.FileMemory)
	if err != nil {
		return fmt.Errorf("reading MEMORY.md: %w", err)
	}

	entry := formatActiveFinding(f)
	
	// Find the Active Findings header
	header := "## Active Findings\n"
	idx := strings.Index(content, header)
	if idx == -1 {
		// Create the section if missing
		content += "\n" + header + "\n" + entry
	} else {
		// Insert right after the header
		insertAt := idx + len(header)
		content = content[:insertAt] + "\n" + entry + "\n" + content[insertAt:]
	}

	// Update the file
	return c.persona.UpdateFile(persona.FileMemory, content)
}

// RunFullCompaction rewrites MEMORY.md completely based on age tiers.
func (c *Compactor) RunFullCompaction() error {
	// Retrieve all active state (let's say up to 1000)
	findings, err := c.store.ListFindings(1000, 0)
	if err != nil {
		return fmt.Errorf("listing findings: %w", err)
	}

	now := time.Now()
	var active, recent, archive []*memory.Finding

	for _, f := range findings {
		ageDays := now.Sub(f.UpdatedAt).Hours() / 24.0

		// Categorize by age (or importance)
		if f.Importance == 2 {
			active = append(active, f) // Permanent findings stay active
		} else if ageDays < 7 {
			active = append(active, f)
		} else if ageDays < 30 {
			recent = append(recent, f)
		} else {
			archive = append(archive, f)
		}
	}

	var sb strings.Builder
	sb.WriteString("# MEMORY.md — Accumulated Knowledge\n\n")

	// 1. Active Findings
	sb.WriteString("## Active Findings\n\n")
	if len(active) == 0 {
		sb.WriteString("(Empty)\n\n")
	} else {
		for _, f := range active {
			sb.WriteString(formatActiveFinding(f))
			sb.WriteString("\n")
		}
	}

	// 2. Recent Knowledge
	sb.WriteString("## Recent Knowledge\n\n")
	if len(recent) == 0 {
		sb.WriteString("(Empty)\n\n")
	} else {
		for _, f := range recent {
			sb.WriteString(formatRecentFinding(f))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// 3. Archived Knowledge
	sb.WriteString("## Archived Knowledge\n\n")
	if len(archive) == 0 {
		sb.WriteString("(Empty)\n\n")
	} else {
		sb.WriteString(formatArchive(archive))
	}

	return c.persona.UpdateFile(persona.FileMemory, sb.String())
}

func importanceStr(imp int) string {
	switch imp {
	case 2:
		return "permanent"
	case 0:
		return "transient"
	default:
		return "normal"
	}
}

func formatActiveFinding(f *memory.Finding) string {
	tagStr := ""
	if f.Tags != "" {
		tagStr = fmt.Sprintf(" [%s]", f.Tags)
	}
	return fmt.Sprintf("### %s%s *%s*\n*%s*\n\n%s\n", 
		f.Key, tagStr, importanceStr(f.Importance), 
		f.UpdatedAt.Format("2006-01-02 15:04"), f.Content)
}

func formatRecentFinding(f *memory.Finding) string {
	tagStr := ""
	if f.Tags != "" {
		tagStr = fmt.Sprintf(" [%s]", f.Tags)
	}
	
	// Compress content
	compressed := strings.ReplaceAll(f.Content, "\n", "; ")
	if len(compressed) > 150 {
		compressed = compressed[:147] + "..."
	}
	
	return fmt.Sprintf("- **%s**%s: %s", f.Key, tagStr, compressed)
}

func formatArchive(findings []*memory.Finding) string {
	groups := make(map[string][]string)

	for _, f := range findings {
		primaryTag := "General"
		if f.Tags != "" {
			parts := strings.SplitN(f.Tags, ",", 2)
			if parts[0] != "" {
				primaryTag = strings.Title(strings.ToLower(strings.TrimSpace(parts[0])))
			}
		}

		compressed := strings.ReplaceAll(f.Content, "\n", "; ")
		if len(compressed) > 100 {
			compressed = compressed[:97] + "..."
		}
		groups[primaryTag] = append(groups[primaryTag], compressed)
	}

	var sb strings.Builder
	for tag, items := range groups {
		sb.WriteString(fmt.Sprintf("### %s\n\n", tag))
		
		// Deduplicate and limit to 10
		seen := make(map[string]bool)
		count := 0
		for _, item := range items {
			itemLower := strings.ToLower(item)
			if !seen[itemLower] {
				seen[itemLower] = true
				sb.WriteString(fmt.Sprintf("- %s\n", item))
				count++
				if count >= 10 {
					break
				}
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
