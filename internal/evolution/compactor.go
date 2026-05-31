// Package evolution implements the background evolution engine and progressive
// summarization compactor for memcp.
package evolution

import (
	"fmt"
	"strings"
	"time"

	"github.com/sivakumar455/memcp/internal/memory"
	"github.com/sivakumar455/memcp/internal/persona"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
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

// AppendToMemory appends a new finding to the Active Findings section of MEMORY.evolved.md.
func (c *Compactor) AppendToMemory(f *memory.Finding) error {
	content, _ := c.persona.ReadFile(persona.FileMemoryEvolved)
	// If evolved file doesn't exist yet, start with a header
	if content == "" {
		content = "# Evolved Memory — System-Generated Findings\n\n## Active Findings\n"
	}

	entry := formatActiveFinding(f)

	header := "## Active Findings\n"
	idx := strings.Index(content, header)
	if idx == -1 {
		content += "\n" + header + "\n" + entry
	} else {
		insertAt := idx + len(header)
		content = content[:insertAt] + "\n" + entry + "\n" + content[insertAt:]
	}

	return c.persona.UpdateFile(persona.FileMemoryEvolved, content)
}

// RunFullCompaction rewrites MEMORY.evolved.md completely based on age tiers.
// The authored MEMORY.md is never touched — only the evolved variant is rewritten.
func (c *Compactor) RunFullCompaction() error {
	findings, err := c.store.ListFindings(1000, 0)
	if err != nil {
		return fmt.Errorf("listing findings: %w", err)
	}

	now := time.Now()
	var active, recent, archive []*memory.Finding

	for _, f := range findings {
		ageDays := now.Sub(f.UpdatedAt).Hours() / 24.0

		if f.Importance == 2 {
			active = append(active, f)
		} else if ageDays < 7 {
			active = append(active, f)
		} else if ageDays < 30 {
			recent = append(recent, f)
		} else {
			archive = append(archive, f)
		}
	}

	var sb strings.Builder
	sb.WriteString("# Evolved Memory — System-Generated Findings\n\n")

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

	newContent := sb.String()

	// Read existing evolved content to avoid unnecessary I/O
	existingContent, _ := c.persona.ReadFile(persona.FileMemoryEvolved)
	if newContent == existingContent {
		return nil
	}

	return c.persona.UpdateFile(persona.FileMemoryEvolved, newContent)
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
					primaryTag = cases.Title(language.English).String(strings.ToLower(strings.TrimSpace(parts[0])))
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
