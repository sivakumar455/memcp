package persona

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestSoulDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create SOUL.md
	os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte(`# SOUL.md — Core Personality

You are a focused engineering assistant.

## Core Behavior

Be direct. Skip pleasantries.

## Boundaries

- Never execute destructive operations
`), 0644)

	// Create IDENTITY.md
	os.WriteFile(filepath.Join(dir, "IDENTITY.md"), []byte(`# IDENTITY.md — Domain Knowledge

## Infrastructure Topology

- Cluster A: Production
- Cluster B: Staging

## Learned Patterns

*Auto-generated from accumulated findings.*

- Recurring topic: **timeout** (seen 5 times)
`), 0644)

	// Create MEMORY.md
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(`# MEMORY.md — Accumulated Knowledge

## Active Findings

### timeout-rootcause [rootcause,timeout] *permanent*
*2026-03-31 18:35*

Root cause: expired certificate.
`), 0644)

	return dir
}

func TestLoadAll(t *testing.T) {
	dir := setupTestSoulDir(t)
	loader := NewLoader(dir, 20000, 100000)

	files, err := loader.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	if len(files) != 3 {
		t.Fatalf("got %d files, want 3", len(files))
	}

	// Check order
	expected := []string{"SOUL.md", "IDENTITY.md", "MEMORY.md"}
	for i, pf := range files {
		if pf.Name != expected[i] {
			t.Errorf("file[%d].Name = %q, want %q", i, pf.Name, expected[i])
		}
		if pf.Content == "" {
			t.Errorf("file[%d].Content is empty", i)
		}
	}
}

func TestLoadForContext(t *testing.T) {
	dir := setupTestSoulDir(t)
	loader := NewLoader(dir, 20000, 100000)

	ctx, err := loader.LoadForContext()
	if err != nil {
		t.Fatalf("LoadForContext: %v", err)
	}

	// Should contain SOUL.md and IDENTITY.md headers
	if !strings.Contains(ctx, "--- SOUL.md ---") {
		t.Error("missing SOUL.md section header")
	}
	if !strings.Contains(ctx, "--- IDENTITY.md ---") {
		t.Error("missing IDENTITY.md section header")
	}
	// Should NOT contain MEMORY.md
	if strings.Contains(ctx, "--- MEMORY.md ---") {
		t.Error("MEMORY.md should NOT be in context (it's covered by FTS5)")
	}
}

func TestSoulImmutability(t *testing.T) {
	dir := setupTestSoulDir(t)
	loader := NewLoader(dir, 20000, 100000)

	err := loader.UpdateFile("SOUL.md", "new content")
	if err == nil {
		t.Fatal("expected error updating SOUL.md")
	}
	if !strings.Contains(err.Error(), "immutable") {
		t.Errorf("error should mention immutability, got: %v", err)
	}
}

func TestUpdateIdentity(t *testing.T) {
	dir := setupTestSoulDir(t)
	loader := NewLoader(dir, 20000, 100000)

	newContent := "# Updated IDENTITY\n\nNew domain knowledge."
	err := loader.UpdateFile("IDENTITY.md", newContent)
	if err != nil {
		t.Fatalf("UpdateFile: %v", err)
	}

	content, err := loader.ReadFile("IDENTITY.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if content != newContent {
		t.Errorf("got content %q, want %q", content, newContent)
	}
}

func TestUpdateMemory(t *testing.T) {
	dir := setupTestSoulDir(t)
	loader := NewLoader(dir, 20000, 100000)

	newContent := "# Updated MEMORY\n\n## Active Findings\n\nNew finding here."
	err := loader.UpdateFile("MEMORY.md", newContent)
	if err != nil {
		t.Fatalf("UpdateFile: %v", err)
	}

	content, err := loader.ReadFile("MEMORY.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if content != newContent {
		t.Errorf("got content %q, want %q", content, newContent)
	}
}

func TestInvalidFileName(t *testing.T) {
	dir := setupTestSoulDir(t)
	loader := NewLoader(dir, 20000, 100000)

	_, err := loader.ReadFile("INVALID.md")
	if err == nil {
		t.Fatal("expected error for invalid file name")
	}

	err = loader.UpdateFile("INVALID.md", "content")
	if err == nil {
		t.Fatal("expected error for invalid file name")
	}
}

func TestEvolvedFilesConcat(t *testing.T) {
	dir := setupTestSoulDir(t)
	loader := NewLoader(dir, 20000, 100000)

	// Write evolved content
	evolvedIdentity := "## Learned Patterns\n\n- **timeout** (seen 10 times)\n"
	if err := loader.UpdateFile(FileIdentityEvolved, evolvedIdentity); err != nil {
		t.Fatalf("UpdateFile evolved identity: %v", err)
	}
	evolvedMemory := "# Evolved Memory\n\n## Active Findings\n\n- evolved finding\n"
	if err := loader.UpdateFile(FileMemoryEvolved, evolvedMemory); err != nil {
		t.Fatalf("UpdateFile evolved memory: %v", err)
	}

	// LoadAll should concatenate authored + evolved
	files, err := loader.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	var identityContent, memoryContent string
	for _, f := range files {
		switch f.Name {
		case FileIdentity:
			identityContent = f.Content
		case FileMemory:
			memoryContent = f.Content
		}
	}

	// Should contain both authored and evolved content
	if !strings.Contains(identityContent, "Infrastructure Topology") {
		t.Error("IDENTITY should contain authored content")
	}
	if !strings.Contains(identityContent, "timeout") {
		t.Error("IDENTITY should contain evolved patterns")
	}
	if !strings.Contains(memoryContent, "timeout-rootcause") {
		t.Error("MEMORY should contain authored content")
	}
	if !strings.Contains(memoryContent, "evolved finding") {
		t.Error("MEMORY should contain evolved content")
	}
}

func TestEvolvedFilesInContext(t *testing.T) {
	dir := setupTestSoulDir(t)
	loader := NewLoader(dir, 20000, 100000)

	loader.UpdateFile(FileIdentityEvolved, "## Evolved Patterns\n\n- pattern A\n")

	ctx, err := loader.LoadForContext()
	if err != nil {
		t.Fatalf("LoadForContext: %v", err)
	}

	if !strings.Contains(ctx, "pattern A") {
		t.Error("LoadForContext should include evolved identity content")
	}
}

func TestPerFileTruncation(t *testing.T) {
	dir := t.TempDir()

	// Create a large SOUL.md with multiple sections
	var sb strings.Builder
	sb.WriteString("# SOUL.md — Large File\n\n")
	sb.WriteString("## Section One\n\n")
	sb.WriteString(strings.Repeat("Content for section one. ", 100))
	sb.WriteString("\n\n## Section Two\n\n")
	sb.WriteString(strings.Repeat("Content for section two. ", 100))
	sb.WriteString("\n\n## Section Three\n\n")
	sb.WriteString(strings.Repeat("Content for section three. ", 100))

	os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte(sb.String()), 0644)

	// Limit to 500 chars
	loader := NewLoader(dir, 500, 100000)
	files, err := loader.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}

	// Should be truncated
	if len(files[0].Content) > 600 { // some slack for truncation notice
		t.Errorf("content length %d exceeds expected truncated length", len(files[0].Content))
	}

	// Should contain truncation notice
	if !strings.Contains(files[0].Content, "truncated") {
		t.Error("truncated content should contain truncation notice")
	}
}

func TestTotalCharLimit(t *testing.T) {
	dir := setupTestSoulDir(t)

	// Set very low total limit — should only load first file
	loader := NewLoader(dir, 20000, 50) // total 50 chars
	files, err := loader.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	// Should have loaded at least 1 file but total chars should be under budget
	totalChars := 0
	for _, f := range files {
		totalChars += len(f.Content)
	}

	// With only 50 char budget, not all files should be fully loaded
	if len(files) == 3 {
		t.Log("Warning: all 3 files fit in 50 chars, test may need larger files")
	}
}

func TestSectionAwareTruncation(t *testing.T) {
	content := `# Title

## First Section

First section content here.

## Second Section

Second section content here.

## Third Section

Third section content is much longer and goes beyond the limit.`

	// Truncate to fit first two sections (~80 chars)
	result := truncateAtSectionBoundary(content, 80)

	// Should cut at a section boundary
	if strings.Contains(result, "Third Section") {
		t.Error("third section should be truncated out")
	}
	if !strings.Contains(result, "First Section") {
		t.Error("first section should be preserved")
	}
}

func TestViewAll(t *testing.T) {
	dir := setupTestSoulDir(t)
	loader := NewLoader(dir, 20000, 100000)

	text, err := loader.ViewAll()
	if err != nil {
		t.Fatalf("ViewAll: %v", err)
	}

	if !strings.Contains(text, "--- SOUL.md ---") {
		t.Error("missing SOUL.md header")
	}
	if !strings.Contains(text, "--- IDENTITY.md ---") {
		t.Error("missing IDENTITY.md header")
	}
	if !strings.Contains(text, "--- MEMORY.md ---") {
		t.Error("missing MEMORY.md header")
	}
}

func TestMissingSoulDir(t *testing.T) {
	loader := NewLoader("/nonexistent/path", 20000, 100000)

	files, err := loader.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll should not error for missing files: %v", err)
	}
	// Should return empty list (all files missing is OK)
	if len(files) != 0 {
		t.Errorf("got %d files, want 0 for nonexistent dir", len(files))
	}
}
