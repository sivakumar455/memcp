package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNewStore(t *testing.T) {
	s := tempStore(t)
	if s.DB() == nil {
		t.Fatal("expected non-nil db")
	}
}

func TestSessionCRUD(t *testing.T) {
	s := tempStore(t)

	// Create
	sess, err := s.CreateSession("test-session")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.Name != "test-session" {
		t.Errorf("got name %q, want %q", sess.Name, "test-session")
	}
	if sess.ID == "" {
		t.Error("expected non-empty ID")
	}

	// Get by ID
	got, err := s.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Name != "test-session" {
		t.Errorf("got name %q, want %q", got.Name, "test-session")
	}

	// Get by name
	got, err = s.GetSessionByName("test-session")
	if err != nil {
		t.Fatalf("GetSessionByName: %v", err)
	}
	if got.ID != sess.ID {
		t.Errorf("got ID %q, want %q", got.ID, sess.ID)
	}

	// List
	sessions, err := s.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("got %d sessions, want 1", len(sessions))
	}
}

func TestFindingCRUD(t *testing.T) {
	s := tempStore(t)

	// Insert
	f := &Finding{
		Key:        "test-finding",
		Content:    "This is a test finding",
		Tags:       "test,unit",
		Importance: 1,
		Source:     "manual",
	}
	if err := s.UpsertFinding(f); err != nil {
		t.Fatalf("UpsertFinding: %v", err)
	}

	// Get by key
	got, err := s.GetFinding("test-finding")
	if err != nil {
		t.Fatalf("GetFinding: %v", err)
	}
	if got.Content != "This is a test finding" {
		t.Errorf("got content %q, want %q", got.Content, "This is a test finding")
	}
	if got.Tags != "test,unit" {
		t.Errorf("got tags %q, want %q", got.Tags, "test,unit")
	}

	// Update (upsert with same key)
	f2 := &Finding{
		Key:        "test-finding",
		Content:    "Updated content",
		Tags:       "test,updated",
		Importance: 2,
		Source:     "manual",
	}
	if err := s.UpsertFinding(f2); err != nil {
		t.Fatalf("UpsertFinding update: %v", err)
	}

	got, err = s.GetFinding("test-finding")
	if err != nil {
		t.Fatalf("GetFinding after update: %v", err)
	}
	if got.Content != "Updated content" {
		t.Errorf("got content %q, want %q", got.Content, "Updated content")
	}
	if got.Importance != 2 {
		t.Errorf("got importance %d, want 2", got.Importance)
	}

	// Count
	count, err := s.CountFindings()
	if err != nil {
		t.Fatalf("CountFindings: %v", err)
	}
	if count != 1 {
		t.Errorf("got count %d, want 1", count)
	}
}

func TestFTSSearch(t *testing.T) {
	s := tempStore(t)

	// Insert multiple findings
	findings := []*Finding{
		{Key: "timeout-rootcause", Content: "Connection timeout due to expired certificate", Tags: "timeout,rootcause", Importance: 2},
		{Key: "deploy-failure", Content: "Deployment failed due to memory leak", Tags: "deployment,failure", Importance: 1},
		{Key: "staging-state", Content: "Staging environment is healthy", Tags: "environment,staging", Importance: 2},
	}
	for _, f := range findings {
		if err := s.UpsertFinding(f); err != nil {
			t.Fatalf("UpsertFinding %s: %v", f.Key, err)
		}
	}

	// Search for "timeout"
	results, err := s.SearchFindings("timeout", 10)
	if err != nil {
		t.Fatalf("SearchFindings: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'timeout'")
	}
	found := false
	for _, r := range results {
		if r.Key == "timeout-rootcause" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'timeout-rootcause' in results")
	}

	// Search for "deployment"
	results, err = s.SearchFindings("deployment", 10)
	if err != nil {
		t.Fatalf("SearchFindings deployment: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'deployment'")
	}

	// Search for "staging"
	results, err = s.SearchFindings("staging", 10)
	if err != nil {
		t.Fatalf("SearchFindings staging: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'staging'")
	}
}

func TestMessages(t *testing.T) {
	s := tempStore(t)

	sess, err := s.CreateSession("msg-test")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Save messages
	for i := 0; i < 5; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if err := s.SaveMessage(sess.ID, role, "message content"); err != nil {
			t.Fatalf("SaveMessage %d: %v", i, err)
		}
	}

	// Get messages
	msgs, err := s.GetMessages(sess.ID, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 5 {
		t.Errorf("got %d messages, want 5", len(msgs))
	}
	// Should be in chronological order
	if msgs[0].Role != "user" {
		t.Errorf("first message role = %q, want %q", msgs[0].Role, "user")
	}
}

func TestToolCalls(t *testing.T) {
	s := tempStore(t)

	tc := &ToolCall{
		ToolName:      "test_tool",
		Backend:       "test-backend",
		ArgsSummary:   "arg1=val1",
		ResultSummary: "success",
		ElapsedMs:     150,
	}
	if err := s.SaveToolCall(tc); err != nil {
		t.Fatalf("SaveToolCall: %v", err)
	}

	calls, err := s.GetRecentToolCalls("", 10)
	if err != nil {
		t.Fatalf("GetRecentToolCalls: %v", err)
	}
	if len(calls) != 1 {
		t.Errorf("got %d calls, want 1", len(calls))
	}
	if calls[0].ToolName != "test_tool" {
		t.Errorf("got tool name %q, want %q", calls[0].ToolName, "test_tool")
	}
}

func TestProfile(t *testing.T) {
	s := tempStore(t)

	// Upsert profile entries
	if err := s.UpsertProfile("tool:kubectl", "kubectl", "tool"); err != nil {
		t.Fatalf("UpsertProfile: %v", err)
	}
	// Hit again
	if err := s.UpsertProfile("tool:kubectl", "kubectl", "tool"); err != nil {
		t.Fatalf("UpsertProfile second: %v", err)
	}

	entries, err := s.GetProfileByCategory("tool", 10)
	if err != nil {
		t.Fatalf("GetProfileByCategory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].HitCount != 2 {
		t.Errorf("got hit_count %d, want 2", entries[0].HitCount)
	}
}

func TestSanitizeContent(t *testing.T) {
	tests := []struct {
		input    string
		contains string
	}{
		{"password=secret123", "[REDACTED]"},
		{"token: abc123def456", "[REDACTED]"},
		{"Bearer eyJhbGciOiJIUzI1NiJ9.test", "[REDACTED]"},
		{"Basic dXNlcjpwYXNz", "[REDACTED]"},
		{"normal content without secrets", "normal content without secrets"},
	}

	for _, tt := range tests {
		result := SanitizeContent(tt.input)
		if tt.contains == "[REDACTED]" {
			if result == tt.input {
				t.Errorf("SanitizeContent(%q) was not sanitized", tt.input)
			}
		} else {
			if result != tt.contains {
				t.Errorf("SanitizeContent(%q) = %q, want %q", tt.input, result, tt.contains)
			}
		}
	}
}

func TestStoreCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "subdir", "nested", "test.db")

	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	// Check the file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file was not created")
	}
}
