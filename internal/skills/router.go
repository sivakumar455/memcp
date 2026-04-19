package skills

import (
	"strings"
)

// Router classifies findings and tool calls into skill domains using a priority chain.
type Router struct {
	// toolPrefixes maps tool name prefixes to domain names (e.g., "webapp_" → "web-app").
	toolPrefixes map[string]string
	// tagMapping maps tags to domain names (e.g., "web" → "web-app").
	tagMapping map[string]string
	// skills loaded from skills directory (for frontmatter-based routing).
	skills []*Skill
}

// NewRouter creates a SkillRouter with config-based and skill-based routing rules.
func NewRouter(skills []*Skill, toolPrefixes, tagMapping map[string]string) *Router {
	if toolPrefixes == nil {
		toolPrefixes = make(map[string]string)
	}
	if tagMapping == nil {
		tagMapping = make(map[string]string)
	}

	// Also extract routing rules from skill frontmatter metadata.
	for _, s := range skills {
		if s.Metadata.ToolPrefixes != "" {
			for _, prefix := range strings.Split(s.Metadata.ToolPrefixes, ",") {
				prefix = strings.TrimSpace(prefix)
				if prefix != "" {
					toolPrefixes[prefix] = s.Name
				}
			}
		}
		if s.Metadata.Tags != "" {
			for _, tag := range strings.Split(s.Metadata.Tags, ",") {
				tag = strings.TrimSpace(strings.ToLower(tag))
				if tag != "" {
					// Don't overwrite config-level mappings.
					if _, exists := tagMapping[tag]; !exists {
						tagMapping[tag] = s.Name
					}
				}
			}
		}
	}

	return &Router{
		toolPrefixes: toolPrefixes,
		tagMapping:   tagMapping,
		skills:       skills,
	}
}

// Classify determines the domain for a finding or tool call using a priority chain:
//  1. Explicit domain (if provided)
//  2. Tool prefix match (longest prefix wins)
//  3. Tag mapping (first matching tag wins)
//  4. No match → ""
func (r *Router) Classify(domain, toolName, tags string) string {
	// Priority 1: Explicit domain
	if domain != "" {
		return domain
	}

	// Priority 2: Tool prefix match (longest prefix wins)
	if toolName != "" {
		bestPrefix := ""
		bestDomain := ""
		lower := strings.ToLower(toolName)
		for prefix, d := range r.toolPrefixes {
			if strings.HasPrefix(lower, strings.ToLower(prefix)) && len(prefix) > len(bestPrefix) {
				bestPrefix = prefix
				bestDomain = d
			}
		}
		if bestDomain != "" {
			return bestDomain
		}
	}

	// Priority 3: Tag mapping
	if tags != "" {
		for _, tag := range strings.Split(tags, ",") {
			tag = strings.TrimSpace(strings.ToLower(tag))
			if d, ok := r.tagMapping[tag]; ok {
				return d
			}
		}
	}

	// Priority 4: No match
	return ""
}

// ScoreForQuery scores skills against a recall query for relevance.
// Returns skills sorted by relevance (highest first) with score > 0.
func (r *Router) ScoreForQuery(query string) []*ScoredSkill {
	if query == "" {
		return nil
	}

	keywords := strings.Fields(strings.ToLower(query))
	var scored []*ScoredSkill

	for _, s := range r.skills {
		score := 0
		nameLower := strings.ToLower(s.Name)
		descLower := strings.ToLower(s.Description)
		tagsLower := strings.ToLower(s.Metadata.Tags)

		for _, kw := range keywords {
			if strings.Contains(nameLower, kw) {
				score += 30
			}
			if strings.Contains(tagsLower, kw) {
				score += 20
			}
			if strings.Contains(descLower, kw) {
				score += 10
			}
		}

		if score > 0 {
			scored = append(scored, &ScoredSkill{Skill: s, Score: score})
		}
	}

	// Sort by score descending
	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].Score > scored[i].Score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	return scored
}

// ScoredSkill pairs a skill with its relevance score.
type ScoredSkill struct {
	Skill *Skill
	Score int
}
