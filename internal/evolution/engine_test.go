package evolution

import (
	"strings"
	"testing"
	"time"

	"github.com/sivakumar455/memcp/internal/config"
	"github.com/sivakumar455/memcp/internal/memory"
	"github.com/sivakumar455/memcp/internal/persona"
)

// Helper to setup dependencies for testing
func setupTestEnv(t *testing.T) (*memory.Store, *persona.Loader, func()) {
	store, err := memory.NewStore(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	pDir := t.TempDir()
	loader := persona.NewLoader(pDir, 20000, 100000)
	// Authored files (user/site-owned)
	loader.UpdateFile(persona.FileIdentity, "# IDENTITY.md\n\nUser domain knowledge.")
	loader.UpdateFile(persona.FileMemory, "# MEMORY.md\n\nUser notes here.")

	cleanup := func() {
		store.Close()
	}
	return store, loader, cleanup
}

func TestEngineRun(t *testing.T) {
	store, loader, cleanup := setupTestEnv(t)
	defer cleanup()

	store.UpsertFinding(&memory.Finding{
		Key:        "issue-timeout-db",
		Content:    "db timeout happened.",
		Tags:       "database,timeout",
		Importance: 1,
	})
	store.UpsertFinding(&memory.Finding{
		Key:        "issue-timeout-api",
		Content:    "api timeout happened.",
		Tags:       "api,timeout",
		Importance: 1,
	})

	cfg := config.EvolutionConfig{
		Enabled:             true,
		MaxIdentityPatterns: 10,
	}

	saveCh := make(chan struct{})
	engine := NewEngine(cfg, store, loader, saveCh, nil)

	err := engine.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Authored IDENTITY.md should be untouched
	ident, err := loader.ReadFile(persona.FileIdentity)
	if err != nil {
		t.Fatalf("ReadFile identity: %v", err)
	}
	if strings.Contains(ident, "timeout") {
		t.Error("authored IDENTITY.md should NOT contain evolved patterns")
	}

	// IDENTITY.evolved.md should contain learned patterns
	identEvolved, err := loader.ReadFile(persona.FileIdentityEvolved)
	if err != nil {
		t.Fatalf("ReadFile identity evolved: %v", err)
	}
	if !strings.Contains(identEvolved, "timeout") {
		t.Errorf("expected IDENTITY.evolved.md to contain 'timeout' pattern, got:\n%s", identEvolved)
	}

	// Authored MEMORY.md should be untouched
	mem, err := loader.ReadFile(persona.FileMemory)
	if err != nil {
		t.Fatalf("ReadFile memory: %v", err)
	}
	if !strings.Contains(mem, "User notes") {
		t.Error("authored MEMORY.md should still contain original user notes")
	}

	// MEMORY.evolved.md should contain compacted findings
	memEvolved, err := loader.ReadFile(persona.FileMemoryEvolved)
	if err != nil {
		t.Fatalf("ReadFile memory evolved: %v", err)
	}
	if !strings.Contains(memEvolved, "issue-timeout-db") {
		t.Errorf("expected MEMORY.evolved.md to contain 'issue-timeout-db', got:\n%s", memEvolved)
	}
}

func TestCompactorAppend(t *testing.T) {
	store, loader, cleanup := setupTestEnv(t)
	defer cleanup()

	compactor := NewCompactor(store, loader)
	f := &memory.Finding{
		Key:        "new-finding-xs",
		Content:    "test append",
		Tags:       "test",
		Importance: 2,
		UpdatedAt:  time.Now(),
	}

	err := compactor.AppendToMemory(f)
	if err != nil {
		t.Fatalf("AppendToMemory: %v", err)
	}

	// Should be appended to MEMORY.evolved.md, not MEMORY.md
	memEvolved, err := loader.ReadFile(persona.FileMemoryEvolved)
	if err != nil {
		t.Fatalf("ReadFile memory evolved: %v", err)
	}
	if !strings.Contains(memEvolved, "new-finding-xs") {
		t.Errorf("append to MEMORY.evolved.md failed")
	}

	// Authored MEMORY.md should remain untouched
	mem, err := loader.ReadFile(persona.FileMemory)
	if err != nil {
		t.Fatalf("ReadFile memory: %v", err)
	}
	if strings.Contains(mem, "new-finding-xs") {
		t.Error("authored MEMORY.md should not be modified by AppendToMemory")
	}
}
