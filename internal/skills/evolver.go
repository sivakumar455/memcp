package skills

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/sivakumar455/memcp/internal/common"
	"github.com/sivakumar455/memcp/internal/memory"
)

// evolverStopWords is aliased from the shared common package.
var evolverStopWords = common.StopWords

// Evolver handles per-skill pattern evolution from domain-filtered findings.
type Evolver struct {
	store      *memory.Store
	manager    *Manager
	maxPatterns int
}

// NewEvolver creates a new SkillEvolver.
func NewEvolver(store *memory.Store, manager *Manager, maxPatterns int) *Evolver {
	if maxPatterns <= 0 {
		maxPatterns = 20
	}
	return &Evolver{
		store:       store,
		manager:     manager,
		maxPatterns: maxPatterns,
	}
}

// EvolveAll evolves all auto_evolve-enabled skills.
func (e *Evolver) EvolveAll() error {
	skills, err := e.manager.LoadAll()
	if err != nil {
		return fmt.Errorf("loading skills: %w", err)
	}

	for _, skill := range skills {
		if !skill.IsAutoEvolve() {
			continue
		}
		if err := e.EvolveSkill(skill); err != nil {
			slog.Error("skill evolution failed", "skill", skill.Name, "error", err)
			// Continue with other skills
		}
	}

	return nil
}

// EvolveSkill evolves a single skill by extracting patterns from domain-filtered findings.
func (e *Evolver) EvolveSkill(skill *Skill) error {
	// Load findings for this skill's domain
	findings, err := e.store.GetFindingsByDomain(skill.Name, 500)
	if err != nil {
		return fmt.Errorf("loading findings for domain %s: %w", skill.Name, err)
	}

	if len(findings) == 0 {
		return nil // Nothing to evolve from
	}

	// Extract patterns (same algorithm as IDENTITY.md but scoped to this domain)
	patternsSection := e.extractPatterns(findings)

	// Replace the "Learned Patterns" section in the skill's markdown body
	body := skill.Markdown
	idx := strings.Index(body, "## Learned Patterns")
	if idx != -1 {
		body = body[:idx]
	}

	body = strings.TrimRight(body, "\n\r\t ") + "\n\n" + patternsSection

	// Write back using UpdateMarkdown (preserves frontmatter)
	if err := skill.UpdateMarkdown(body); err != nil {
		return fmt.Errorf("updating skill %s: %w", skill.Name, err)
	}

	slog.Info("evolved skill", "skill", skill.Name, "findings", len(findings))
	return nil
}

func (e *Evolver) extractPatterns(findings []*memory.Finding) string {
	tagCounts := make(map[string]int)
	wordCounts := make(map[string]int)

	for _, f := range findings {
		// Count tags
		if f.Tags != "" {
			for _, tag := range strings.Split(f.Tags, ",") {
				t := strings.ToLower(strings.TrimSpace(tag))
				if t != "" {
					tagCounts[t]++
				}
			}
		}

		// Extract keywords from keys
		words := strings.FieldsFunc(f.Key, func(r rune) bool {
			return r == '-' || r == '_' || r == '/' || r == ':' || r == ' '
		})
		for _, w := range words {
			w = strings.ToLower(w)
			if !evolverStopWords[w] && len(w) > 2 {
				wordCounts[w]++
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("## Learned Patterns\n\n*Auto-generated from domain-tagged findings. Updated by skill evolution.*\n\n")

	// Recurring topics (tags seen >= 2)
	topics := sortedEntries(tagCounts, 2)
	half := e.maxPatterns / 2

	if len(topics) > 0 {
		for i, topic := range topics {
			if i >= half {
				break
			}
			sb.WriteString(fmt.Sprintf("- Recurring topic: **%s** (seen %d times)\n", topic.key, topic.val))
		}
		sb.WriteString("\n")
	}

	// Frequent keywords (seen >= 3)
	keywords := sortedEntries(wordCounts, 3)
	if len(keywords) > 0 {
		for i, kw := range keywords {
			if i >= half {
				break
			}
			sb.WriteString(fmt.Sprintf("- Frequent keyword: **%s** (seen %d times)\n", kw.key, kw.val))
		}
		sb.WriteString("\n")
	}

	if len(topics) == 0 && len(keywords) == 0 {
		sb.WriteString("(No domain-specific patterns learned yet.)\n")
	}

	return sb.String()
}

type kvEntry struct {
	key string
	val int
}

func sortedEntries(m map[string]int, threshold int) []kvEntry {
	var entries []kvEntry
	for k, v := range m {
		if v >= threshold {
			entries = append(entries, kvEntry{k, v})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].val == entries[j].val {
			return entries[i].key < entries[j].key
		}
		return entries[i].val > entries[j].val
	})
	return entries
}
