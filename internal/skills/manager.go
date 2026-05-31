// Package skills implements domain routing and skill evolution for memcp.
package skills

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill represents a loaded skill from a SKILL.md file.
type Skill struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Metadata    Metadata `yaml:"metadata"`
	Markdown    string   `yaml:"-"` // Body
	Path        string   `yaml:"-"`
}

// Metadata holds the routing and configuration options for a skill.
type Metadata struct {
	ToolPrefixes string `yaml:"tool_prefixes"`
	Tags         string `yaml:"tags"`
	AutoEvolve   string `yaml:"auto_evolve"` // "true" or "false"
}

// Manager handles loading and routing for skills.
type Manager struct {
	dir string
}

// NewManager creates a new Skills Manager.
func NewManager(dir string) *Manager {
	return &Manager{dir: dir}
}

// LoadAll loads all skills from the skills directory.
// For each skill, if a SKILL.evolved.md exists alongside SKILL.md the evolved
// content is appended to the Markdown body (authored first, evolved second).
func (m *Manager) LoadAll() ([]*Skill, error) {
	if err := os.MkdirAll(m.dir, 0755); err != nil {
		return nil, fmt.Errorf("creating skills dir: %w", err)
	}

	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, fmt.Errorf("reading skills dir: %w", err)
	}

	var skills []*Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillDir := filepath.Join(m.dir, entry.Name())
		skillFile := filepath.Join(skillDir, "SKILL.md")

		data, err := os.ReadFile(skillFile)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading skill %s: %w", entry.Name(), err)
		}

		skill, err := ParseSkillFile(data)
		if err != nil {
			return nil, fmt.Errorf("parsing skill %s: %w", entry.Name(), err)
		}
		skill.Path = skillFile

		// Merge evolved content if present
		mergeEvolvedContent(skill)

		skills = append(skills, skill)
	}

	return skills, nil
}

// LoadForContext extracts the truncated skill markdown for those matching prefixes/tags.
func (m *Manager) LoadForContext(maxChars int, query, toolCalls string) (string, error) {
	// We just load and format them.
	skills, err := m.LoadAll()
	if err != nil {
		return "", err
	}

	if len(skills) == 0 {
		return "", nil
	}

	var sb strings.Builder
	for _, s := range skills {
		sb.WriteString(fmt.Sprintf("\n--- SKILL: %s ---\n%s\n", s.Name, s.Markdown))
	}

	text := sb.String()
	if len(text) > maxChars && maxChars > 0 {
		return text[:maxChars] + "\n...[truncated]", nil
	}
	return text, nil
}

// EvolvedPath returns the path to the SKILL.evolved.md file alongside SKILL.md.
func (s *Skill) EvolvedPath() string {
	return filepath.Join(filepath.Dir(s.Path), "SKILL.evolved.md")
}

// IsAutoEvolve checks if the skill should be auto-evolved.
func (s *Skill) IsAutoEvolve() bool {
	return strings.ToLower(s.Metadata.AutoEvolve) != "false"
}

// UpdateMarkdown writes back the YAML frontmatter plus new markdown body.
func (s *Skill) UpdateMarkdown(newBody string) error {
	var sb bytes.Buffer
	sb.WriteString("---\n")

	// Create a wrapper struct exactly matching the YAML layout we want
	wrapper := struct {
		Name        string   `yaml:"name"`
		Description string   `yaml:"description"`
		Metadata    Metadata `yaml:"metadata"`
	}{
		Name:        s.Name,
		Description: s.Description,
		Metadata:    s.Metadata,
	}

	encoder := yaml.NewEncoder(&sb)
	if err := encoder.Encode(wrapper); err != nil {
		return err
	}
	sb.WriteString("---\n\n")
	sb.WriteString(newBody)

	return os.WriteFile(s.Path, sb.Bytes(), 0644)
}

// ParseSkillFile takes the raw bytes of a SKILL.md file and splits frontmatter/body.
func ParseSkillFile(data []byte) (*Skill, error) {
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		return nil, fmt.Errorf("missing YAML frontmatter")
	}

	// Find the end of frontmatter
	endIdx := strings.Index(content[4:], "---\n")
	if endIdx == -1 {
		return nil, fmt.Errorf("missing YAML frontmatter closing '---'")
	}

	endIdx += 4 // account for the 4 bytes we skipped
	frontmatter := content[4:endIdx]
	body := content[endIdx+4:]

	var s Skill
	if err := yaml.Unmarshal([]byte(frontmatter), &s); err != nil {
		return nil, err
	}
	s.Markdown = strings.TrimSpace(body)

	return &s, nil
}

// Create creates a new skill directory and SKILL.md file.
func (m *Manager) Create(name, description, tags string) error {
	skillDir := filepath.Join(m.dir, name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("creating skill directory: %w", err)
	}

	skillFile := filepath.Join(skillDir, "SKILL.md")
	if _, err := os.Stat(skillFile); err == nil {
		return fmt.Errorf("skill %q already exists", name)
	}

	autoEvolve := "true"
	if description == "" {
		description = name + " domain"
	}

	var sb bytes.Buffer
	sb.WriteString("---\n")
	wrapper := struct {
		Name        string   `yaml:"name"`
		Description string   `yaml:"description"`
		Metadata    Metadata `yaml:"metadata"`
	}{
		Name:        name,
		Description: description,
		Metadata: Metadata{
			Tags:       tags,
			AutoEvolve: autoEvolve,
		},
	}

	encoder := yaml.NewEncoder(&sb)
	if err := encoder.Encode(wrapper); err != nil {
		return fmt.Errorf("encoding frontmatter: %w", err)
	}
	sb.WriteString("---\n\n")
	sb.WriteString(fmt.Sprintf("## %s Domain Knowledge\n\n", name))
	sb.WriteString("(Add domain-specific knowledge here)\n\n")
	sb.WriteString("## Learned Patterns\n\n")
	sb.WriteString("*Auto-generated from domain-tagged findings. Updated by skill evolution.*\n\n")
	sb.WriteString("(No domain-specific patterns learned yet.)\n")

	return os.WriteFile(skillFile, sb.Bytes(), 0644)
}

// MergeSkills merges the source skill's learned patterns into the target skill.
// Patterns are deduplicated by case-insensitive line matching.
func (m *Manager) MergeSkills(source, target *Skill) error {
	// Extract learned patterns from source
	srcPatterns := extractLearnedPatterns(source.Markdown)
	if len(srcPatterns) == 0 {
		return nil // nothing to merge
	}

	// Find existing patterns in target
	tgtBody := target.Markdown
	idx := strings.Index(tgtBody, "## Learned Patterns")
	var existingPatterns []string
	var bodyBeforePatterns string
	if idx != -1 {
		existingPatterns = extractLearnedPatterns(tgtBody[idx:])
		bodyBeforePatterns = strings.TrimRight(tgtBody[:idx], "\n\r\t ")
	} else {
		bodyBeforePatterns = strings.TrimRight(tgtBody, "\n\r\t ")
	}

	// Deduplicate
	seen := make(map[string]bool)
	for _, p := range existingPatterns {
		seen[strings.ToLower(strings.TrimSpace(p))] = true
	}

	var newPatterns []string
	for _, p := range srcPatterns {
		if !seen[strings.ToLower(strings.TrimSpace(p))] {
			newPatterns = append(newPatterns, p)
			seen[strings.ToLower(strings.TrimSpace(p))] = true
		}
	}

	if len(newPatterns) == 0 {
		return nil // all patterns already present
	}

	// Rebuild body with merged patterns
	var sb strings.Builder
	sb.WriteString(bodyBeforePatterns)
	sb.WriteString("\n\n## Learned Patterns\n\n*Auto-generated from domain-tagged findings. Updated by skill evolution.*\n\n")
	for _, p := range existingPatterns {
		sb.WriteString(p + "\n")
	}
	for _, p := range newPatterns {
		sb.WriteString(p + "\n")
	}

	return target.UpdateMarkdown(sb.String())
}

// Dir returns the skills directory path.
func (m *Manager) Dir() string { return m.dir }

// Discover is an alias for LoadAll that returns skills with populated Dir fields.
func (m *Manager) Discover() ([]*Skill, error) { return m.LoadAll() }

// LoadSkill loads a single skill by name (merging any evolved content).
func (m *Manager) LoadSkill(name string) (*Skill, error) {
	skillFile := filepath.Join(m.dir, name, "SKILL.md")
	data, err := os.ReadFile(skillFile)
	if err != nil {
		return nil, fmt.Errorf("reading skill %s: %w", name, err)
	}
	skill, err := ParseSkillFile(data)
	if err != nil {
		return nil, fmt.Errorf("parsing skill %s: %w", name, err)
	}
	skill.Path = skillFile
	mergeEvolvedContent(skill)
	return skill, nil
}

// UpdateSkill updates a skill's SKILL.md content by name.
func (m *Manager) UpdateSkill(name, content string) error {
	skill, err := m.LoadSkill(name)
	if err != nil {
		return err
	}
	return skill.UpdateMarkdown(content)
}

// ReadSkillFile reads an arbitrary file from a skill directory.
func (m *Manager) ReadSkillFile(name, filename string) (string, error) {
	p := filepath.Join(m.dir, name, filename)
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Tags returns the skill's tags as a slice.
func (s *Skill) Tags() []string {
	if s.Metadata.Tags == "" {
		return nil
	}
	parts := strings.Split(s.Metadata.Tags, ",")
	tags := make([]string, 0, len(parts))
	for _, t := range parts {
		t = strings.TrimSpace(t)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

// ToolPrefixes returns the skill's tool prefixes as a slice.
func (s *Skill) ToolPrefixes() []string {
	if s.Metadata.ToolPrefixes == "" {
		return nil
	}
	parts := strings.Split(s.Metadata.ToolPrefixes, ",")
	prefixes := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			prefixes = append(prefixes, p)
		}
	}
	return prefixes
}

// MatchesQuery returns true if the skill is relevant to the given query.
func (s *Skill) MatchesQuery(query string) bool {
	q := strings.ToLower(query)
	return strings.Contains(strings.ToLower(s.Name), q) ||
		strings.Contains(strings.ToLower(s.Description), q) ||
		strings.Contains(strings.ToLower(s.Metadata.Tags), q)
}

// mergeEvolvedContent appends SKILL.evolved.md content to a loaded skill's Markdown.
func mergeEvolvedContent(skill *Skill) {
	evolvedData, err := os.ReadFile(skill.EvolvedPath())
	if err != nil {
		return // no evolved file — nothing to merge
	}
	evolved := strings.TrimSpace(string(evolvedData))
	if evolved != "" {
		skill.Markdown = strings.TrimRight(skill.Markdown, "\n\r\t ") + "\n\n" + evolved
	}
}

// extractLearnedPatterns extracts bullet-point lines from a "Learned Patterns" section.
func extractLearnedPatterns(body string) []string {
	idx := strings.Index(body, "## Learned Patterns")
	if idx == -1 {
		return nil
	}

	section := body[idx:]
	lines := strings.Split(section, "\n")
	var patterns []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			patterns = append(patterns, trimmed)
		}
	}
	return patterns
}
