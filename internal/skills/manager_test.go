package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillsManager(t *testing.T) {
	dir := t.TempDir()

	// Create a mock skill directory
	skillDir := filepath.Join(dir, "web-app")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	mockSkill := `---
name: Web App Support
description: Skills for managing the web app
metadata:
  tool_prefixes: webapp_
  tags: web
  auto_evolve: true
---
## Patterns
- Pattern 1
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(mockSkill), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mgr := NewManager(dir)
	skills, err := mgr.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}

	s := skills[0]
	if s.Name != "Web App Support" {
		t.Errorf("got name %q", s.Name)
	}
	if s.Metadata.ToolPrefixes != "webapp_" {
		t.Errorf("got prefix %q", s.Metadata.ToolPrefixes)
	}
	if !s.IsAutoEvolve() {
		t.Errorf("expected auto_evolve to be true")
	}

	// Test UpdateMarkdown
	err = s.UpdateMarkdown("## Patterns\n- Updated")
	if err != nil {
		t.Fatalf("UpdateMarkdown: %v", err)
	}

	// Reload to verify
	skills2, err := mgr.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll 2: %v", err)
	}
	if skills2[0].Markdown != "## Patterns\n- Updated" {
		t.Errorf("markdown not updated, got: %v", skills2[0].Markdown)
	}
}

func TestParseSkillNoFrontmatter(t *testing.T) {
	_, err := ParseSkillFile([]byte("just some text"))
	if err == nil {
		t.Error("expected error for missing frontmatter")
	}
}
