package skills

import (
	"fmt"
	"log/slog"
	"os"
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

// EvolveSkill evolves a single skill by extracting patterns from domain-filtered
// findings and writing them to SKILL.evolved.md (never modifying the authored SKILL.md).
func (e *Evolver) EvolveSkill(skill *Skill) error {
	findings, err := e.store.GetFindingsByDomain(skill.Name, 500)
	if err != nil {
		return fmt.Errorf("loading findings for domain %s: %w", skill.Name, err)
	}

	if len(findings) == 0 {
		return nil
	}

	patternsSection := e.extractPatterns(findings)

	// Write evolved patterns to SKILL.evolved.md alongside the authored SKILL.md
	evolvedPath := skill.EvolvedPath()
	if err := os.WriteFile(evolvedPath, []byte(patternsSection), 0644); err != nil {
		return fmt.Errorf("writing evolved skill %s: %w", skill.Name, err)
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

// SkillEvolveResult holds the outcome of a single skill evolution.
type SkillEvolveResult struct {
	SkillName    string
	PatternsAdded int
	Skipped      bool
	SkipReason   string
}

// EvolveAllWithResults evolves all skills and returns per-skill results.
func (e *Evolver) EvolveAllWithResults() ([]SkillEvolveResult, error) {
	skills, err := e.manager.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("loading skills: %w", err)
	}
	var results []SkillEvolveResult
	for _, skill := range skills {
		if !skill.IsAutoEvolve() {
			results = append(results, SkillEvolveResult{SkillName: skill.Name, Skipped: true, SkipReason: "auto_evolve disabled"})
			continue
		}
		if err := e.EvolveSkill(skill); err != nil {
			slog.Error("skill evolution failed", "skill", skill.Name, "error", err)
			results = append(results, SkillEvolveResult{SkillName: skill.Name, Skipped: true, SkipReason: err.Error()})
			continue
		}
		results = append(results, SkillEvolveResult{SkillName: skill.Name})
	}
	return results, nil
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
