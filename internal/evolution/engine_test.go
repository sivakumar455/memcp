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
	// Store
	store, err := memory.NewStore(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Persona loader
	pDir := t.TempDir()
	loader := persona.NewLoader(pDir, 20000, 100000)
	loader.UpdateFile(persona.FileIdentity, "# IDENTITY.md\n\n## Learned Patterns\n\nInitial config.")
	loader.UpdateFile(persona.FileMemory, "# MEMORY.md\n\n## Active Findings\n\n- Finding 1\n")

	cleanup := func() {
		store.Close()
	}
	return store, loader, cleanup
}

func TestEngineRun(t *testing.T) {
	store, loader, cleanup := setupTestEnv(t)
	defer cleanup()

	// Add dummy findings
	store.UpsertFinding(&memory.Finding{
		Key:     "issue-timeout-db",
		Content: "db timeout happened.",
		Tags:    "database,timeout",
		Importance: 1,
	})
	store.UpsertFinding(&memory.Finding{
		Key:     "issue-timeout-api",
		Content: "api timeout happened.",
		Tags:    "api,timeout",
		Importance: 1,
	})

	cfg := config.EvolutionConfig{
		Enabled: true,
		MaxIdentityPatterns: 10,
	}
	
	saveCh := make(chan struct{})
	engine := NewEngine(cfg, store, loader, saveCh, nil)
	
	err := engine.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Check if IDENTITY.md was updated
	ident, err := loader.ReadFile(persona.FileIdentity)
	if err != nil {
		t.Fatalf("ReadFile identity: %v", err)
	}

	if !strings.Contains(ident, "timeout") {
		t.Errorf("expected IDENTITY.md to contain 'timeout' pattern, got:\n%s", ident)
	}

	// Check if MEMORY.md was updated (since Run runs full compaction)
	mem, err := loader.ReadFile(persona.FileMemory)
	if err != nil {
		t.Fatalf("ReadFile memory: %v", err)
	}
	
	if !strings.Contains(mem, "issue-timeout-db") {
		t.Errorf("expected MEMORY.md to contain 'issue-timeout-db', got:\n%s", mem)
	}
}

func TestCompactorAppend(t *testing.T) {
	store, loader, cleanup := setupTestEnv(t)
	defer cleanup()

	compactor := NewCompactor(store, loader)
	f := &memory.Finding{
		Key: "new-finding-xs",
		Content: "test append",
		Tags: "test",
		Importance: 2,
		UpdatedAt: time.Now(),
	}

	err := compactor.AppendToMemory(f)
	if err != nil {
		t.Fatalf("AppendToMemory: %v", err)
	}

	mem, err := loader.ReadFile(persona.FileMemory)
	if err != nil {
		t.Fatalf("ReadFile memory: %v", err)
	}

	if !strings.Contains(mem, "new-finding-xs") {
		t.Errorf("append failed")
	}
}
