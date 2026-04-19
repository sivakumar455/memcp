package engine

import (
	"path/filepath"
	"testing"

	"github.com/sivakumar455/memcp/internal/config"
	"github.com/sivakumar455/memcp/internal/memory"
)

func tempMemoryManager(t *testing.T) (*MemoryManager, *memory.Store) {
	t.Helper()
	dir := t.TempDir()
	s, err := memory.NewStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return NewMemoryManager(s), s
}

func TestMemoryManager_ADD(t *testing.T) {
	mm, _ := tempMemoryManager(t)

	result, err := mm.SaveExplicit("test-key", "test content", "tag1,tag2", 1, "", "", "manual")
	if err != nil {
		t.Fatalf("SaveExplicit: %v", err)
	}
	if result.Action != "ADD" {
		t.Errorf("got action %q, want ADD", result.Action)
	}
}

func TestMemoryManager_NOOP(t *testing.T) {
	mm, _ := tempMemoryManager(t)

	// First save
	_, err := mm.SaveExplicit("test-key", "test content", "tag1", 1, "", "", "manual")
	if err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Same content again → NOOP
	result, err := mm.SaveExplicit("test-key", "test content", "tag1", 1, "", "", "manual")
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if result.Action != "NOOP" {
		t.Errorf("got action %q, want NOOP", result.Action)
	}
}

func TestMemoryManager_UPDATE(t *testing.T) {
	mm, store := tempMemoryManager(t)

	// First save
	_, err := mm.SaveExplicit("test-key", "original content", "tag1", 1, "", "", "manual")
	if err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Different content → UPDATE
	result, err := mm.SaveExplicit("test-key", "new additional info", "tag1,tag2", 1, "", "", "manual")
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if result.Action != "UPDATE" {
		t.Errorf("got action %q, want UPDATE", result.Action)
	}

	// Verify merged content
	f, err := store.GetFinding("test-key")
	if err != nil {
		t.Fatalf("GetFinding: %v", err)
	}
	if f.Content != "original content\nnew additional info" {
		t.Errorf("got content %q, want merged", f.Content)
	}
	if f.Tags != "tag1,tag2" {
		t.Errorf("got tags %q, want %q", f.Tags, "tag1,tag2")
	}
}

func TestMemoryManager_CredentialSanitization(t *testing.T) {
	mm, store := tempMemoryManager(t)

	_, err := mm.SaveExplicit("secret-finding", "password=hunter2 and token: abc123def456", "", 1, "", "", "manual")
	if err != nil {
		t.Fatalf("SaveExplicit: %v", err)
	}

	f, err := store.GetFinding("secret-finding")
	if err != nil {
		t.Fatalf("GetFinding: %v", err)
	}
	if f.Content == "password=hunter2 and token: abc123def456" {
		t.Error("credentials were not sanitized")
	}
}

func TestMemoryManager_ImportanceEscalation(t *testing.T) {
	mm, store := tempMemoryManager(t)

	// Save with normal importance
	_, err := mm.SaveExplicit("test-key", "content", "", 1, "", "", "manual")
	if err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Update with permanent importance
	_, err = mm.SaveExplicit("test-key", "more content", "", 2, "", "", "manual")
	if err != nil {
		t.Fatalf("second save: %v", err)
	}

	f, err := store.GetFinding("test-key")
	if err != nil {
		t.Fatalf("GetFinding: %v", err)
	}
	if f.Importance != 2 {
		t.Errorf("got importance %d, want 2 (should escalate)", f.Importance)
	}
}

func TestEngine_RecallAndSave(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Memory: config.MemoryConfig{
			DBPath:          filepath.Join(dir, "test.db"),
			ContextWindow:   50,
			MaxContextChars: 80000,
		},
		Context: config.ContextConfig{
			MaxChars:       80000,
			CoreBudgetPct:  20,
			WorkBudgetPct:  30,
			RelevBudgetPct: 30,
			HistBudgetPct:  20,
		},
	}

	eng, err := New(cfg)
	if err != nil {
		t.Fatalf("New engine: %v", err)
	}
	defer eng.Close()

	// Save a finding
	result, err := eng.Save("test-finding", "Found root cause: expired certificate", "rootcause,timeout", 2, "")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if result.Action != "ADD" {
		t.Errorf("got action %q, want ADD", result.Action)
	}

	// Recall
	ctx, err := eng.Recall("timeout certificate", "")
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if ctx == "" {
		t.Error("expected non-empty recall context")
	}
	// Should contain our finding
	if !containsString(ctx, "expired certificate") {
		t.Error("recall context should contain the saved finding")
	}
}

func containsString(haystack, needle string) bool {
	return len(haystack) > 0 && len(needle) > 0 && 
		(haystack == needle || len(haystack) > len(needle) && containsSubstring(haystack, needle))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
