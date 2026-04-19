package observation

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sivakumar455/memcp/internal/config"
	"github.com/sivakumar455/memcp/internal/engine"
	"github.com/sivakumar455/memcp/internal/memory"
)

func setupTestEnv(t *testing.T) (*memory.Store, *engine.MemoryManager, func()) {
	// Store
	store, err := memory.NewStore(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	mgr := engine.NewMemoryManager(store)
	cleanup := func() {
		store.Close()
	}
	return store, mgr, cleanup
}

func TestExtractFromArgs(t *testing.T) {
	store, mgr, cleanup := setupTestEnv(t)
	defer cleanup()

	cfg := config.ObservationConfig{
		Enabled: true,
	}

	obs := New(store, mgr, cfg, nil)

	args := map[string]interface{}{
		"env":     "staging",
		"badArg":  "value",
		"ms_name": "users-api",
		"pod":     "users-api-1234-5678",
	}
	rawArgs, _ := json.Marshal(args)

	facts := obs.extractFromArgs(string(rawArgs))
	if len(facts) != 3 {
		t.Fatalf("expected 3 facts, got %d", len(facts))
	}

	foundEnv := false
	foundMs := false
	for _, f := range facts {
		if strings.HasPrefix(f.Key, "env:") {
			foundEnv = true
		}
		if strings.HasPrefix(f.Key, "ms:") {
			foundMs = true
		}
	}

	if !foundEnv {
		t.Errorf("missing env fact")
	}
	if !foundMs {
		t.Errorf("missing ms fact")
	}
}

func TestExtractFromResult(t *testing.T) {
	store, mgr, cleanup := setupTestEnv(t)
	defer cleanup()

	cfg := config.ObservationConfig{
		Enabled: true,
	}
	obs := New(store, mgr, cfg, nil)

	resultStr := `
	Connecting to service...
	Connection failed: timeout reading headers.
	Stack trace: java.net.SocketTimeoutException: timeout
	Trace ID: x-b3-traceid: 1234567890abcdef1234567890abcdef
	`

	facts := obs.extractFromResult("debug_tool", resultStr)
	
	if len(facts) != 2 {
		t.Fatalf("expected 2 facts (1 error block, 1 traceid), got %d", len(facts))
	}

	var traceFact, errFact *extractedFact
	for i, f := range facts {
		if strings.Contains(f.Key, "trace:") {
			traceFact = &facts[i]
		}
		if strings.Contains(f.Key, "obs:") {
			errFact = &facts[i]
		}
	}

	if traceFact == nil || traceFact.Key != "trace:1234567890abcdef1234567890abcdef" {
		t.Errorf("incorrect trace fact: %+v", traceFact)
	}

	if errFact == nil || !strings.Contains(errFact.Content, "timeout") {
		t.Errorf("incorrect error fact: %+v", errFact)
	}
}

func TestSummarizeString(t *testing.T) {
	longStr := strings.Repeat("A", 100)
	sum := summarizeString(longStr, 20)
	
	// max=20. start=14. [...] is 7. rem=-1 -> keepEnd is 0. So it should just return the first 20.
	if sum != strings.Repeat("A", 20) {
		t.Errorf("summary incorrect, got: %s", sum)
	}
	
	sum2 := summarizeString(longStr, 30) // start=21. end=30-21-7 = 2. -> 21 + 7 + 2 = 30.
	expected := strings.Repeat("A", 21) + " [...] " + strings.Repeat("A", 2)
	if sum2 != expected {
		t.Errorf("summary2 incorrect, got: %s", sum2)
	}
}

func TestObserverIntegration(t *testing.T) {
	store, mgr, cleanup := setupTestEnv(t)
	defer cleanup()

	cfg := config.ObservationConfig{
		Enabled: true,
		AsyncUpsert: false,
	}
	obs := New(store, mgr, cfg, nil)

	args := `{"env": "production"}`
	result := "OOMKilled detected on pod 123"
	
	obs.Observe("check_pods", "k8s-mcp", args, result, 100)

	// verify tool call
	tcs, _ := store.GetRecentToolCalls("", 10)
	if len(tcs) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(tcs))
	}
	if tcs[0].ToolName != "check_pods" {
		t.Errorf("incorrect tool name: %s", tcs[0].ToolName)
	}

	// verify memory
	findings, _ := store.GetAllFindings(10)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings (env and OOM error), got %d", len(findings))
	}
}
