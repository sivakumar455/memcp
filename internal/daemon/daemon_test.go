package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sivakumar455/memcp/internal/config"
	"github.com/sivakumar455/memcp/internal/memory"
)

type mockWatcher struct {
	name   string
	events []Event
	called int
}

func (m *mockWatcher) Name() string { return m.name }
func (m *mockWatcher) Poll(ctx context.Context) ([]Event, error) {
	m.called++
	return m.events, nil
}
func (m *mockWatcher) Close() error { return nil }

func newTestStore(t *testing.T) *memory.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := memory.NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestTaskStoreCRUD(t *testing.T) {
	store := newTestStore(t)
	ts := NewTaskStore(store)

	task := Task{
		ID:       "test-1",
		Source:   "jira",
		SourceID: "BUG-100",
		Title:    "Test defect",
		Summary:  "NullPointerException in service",
		Priority: "high",
		Category: "bug",
		Status:   "pending",
	}
	if err := ts.Create(task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := ts.Get("test-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Title != "Test defect" {
		t.Errorf("Title = %q, want %q", got.Title, "Test defect")
	}
	if got.Status != "pending" {
		t.Errorf("Status = %q, want %q", got.Status, "pending")
	}

	exists, err := ts.Exists("jira", "BUG-100")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Error("Exists returned false, want true")
	}

	notExists, err := ts.Exists("jira", "BUG-999")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if notExists {
		t.Error("Exists returned true for non-existent task")
	}

	count, err := ts.PendingCount()
	if err != nil {
		t.Fatalf("PendingCount: %v", err)
	}
	if count != 1 {
		t.Errorf("PendingCount = %d, want 1", count)
	}

	if err := ts.UpdateStatus("test-1", "in_progress", ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, _ = ts.Get("test-1")
	if got.Status != "in_progress" {
		t.Errorf("Status after update = %q, want %q", got.Status, "in_progress")
	}

	if err := ts.UpdateStatus("test-1", "completed", "fixed the NPE"); err != nil {
		t.Fatalf("UpdateStatus complete: %v", err)
	}
	got, _ = ts.Get("test-1")
	if got.Status != "completed" {
		t.Errorf("Status = %q, want %q", got.Status, "completed")
	}
	if got.ActionResult != "fixed the NPE" {
		t.Errorf("ActionResult = %q, want %q", got.ActionResult, "fixed the NPE")
	}
}

func TestTaskStoreSnooze(t *testing.T) {
	store := newTestStore(t)
	ts := NewTaskStore(store)

	ts.Create(Task{ID: "snz-1", Source: "jira", SourceID: "TASK-1", Title: "Snooze me", Summary: "s", Status: "pending"})

	until := time.Now().Add(-1 * time.Second)
	if err := ts.Snooze("snz-1", until); err != nil {
		t.Fatalf("Snooze: %v", err)
	}
	got, _ := ts.Get("snz-1")
	if got.Status != "snoozed" {
		t.Errorf("Status = %q, want snoozed", got.Status)
	}

	unsnoozed, err := ts.UnsnoozeReady()
	if err != nil {
		t.Fatalf("UnsnoozeReady: %v", err)
	}
	if unsnoozed != 1 {
		t.Errorf("UnsnoozeReady = %d, want 1", unsnoozed)
	}
	got, _ = ts.Get("snz-1")
	if got.Status != "pending" {
		t.Errorf("Status after unsnooze = %q, want pending", got.Status)
	}
}

func TestTaskStoreCounts(t *testing.T) {
	store := newTestStore(t)
	ts := NewTaskStore(store)

	ts.Create(Task{ID: "c1", Source: "jira", SourceID: "A-1", Title: "T1", Summary: "s", Status: "pending", Priority: "high"})
	ts.Create(Task{ID: "c2", Source: "jira", SourceID: "A-2", Title: "T2", Summary: "s", Status: "pending", Priority: "normal"})
	ts.Create(Task{ID: "c3", Source: "email", SourceID: "E-1", Title: "T3", Summary: "s", Status: "completed"})

	counts, err := ts.Counts()
	if err != nil {
		t.Fatalf("Counts: %v", err)
	}
	if counts.Pending != 2 {
		t.Errorf("Pending = %d, want 2", counts.Pending)
	}
	if counts.Completed != 1 {
		t.Errorf("Completed = %d, want 1", counts.Completed)
	}
	if counts.Total != 3 {
		t.Errorf("Total = %d, want 3", counts.Total)
	}
}

func TestTaskStoreListFilters(t *testing.T) {
	store := newTestStore(t)
	ts := NewTaskStore(store)

	ts.Create(Task{ID: "f1", Source: "jira", SourceID: "J-1", Title: "Jira high", Summary: "s", Status: "pending", Priority: "high"})
	ts.Create(Task{ID: "f2", Source: "email", SourceID: "E-1", Title: "Email normal", Summary: "s", Status: "pending", Priority: "normal"})
	ts.Create(Task{ID: "f3", Source: "jira", SourceID: "J-2", Title: "Jira critical", Summary: "s", Status: "pending", Priority: "critical"})

	tasks, _ := ts.List(TaskFilter{Source: "jira"})
	if len(tasks) != 2 {
		t.Errorf("List(source=jira) = %d, want 2", len(tasks))
	}

	tasks, _ = ts.List(TaskFilter{Priority: "critical"})
	if len(tasks) != 1 {
		t.Errorf("List(priority=critical) = %d, want 1", len(tasks))
	}

	tasks, _ = ts.List(TaskFilter{})
	if len(tasks) != 3 {
		t.Fatalf("List(all) = %d, want 3", len(tasks))
	}
	if tasks[0].Priority != "critical" {
		t.Errorf("First task priority = %q, want critical", tasks[0].Priority)
	}
}

func TestTaskStoreCleanup(t *testing.T) {
	store := newTestStore(t)
	ts := NewTaskStore(store)

	ts.Create(Task{ID: "old1", Source: "jira", SourceID: "O-1", Title: "Old", Summary: "s", Status: "completed"})

	removed, err := ts.Cleanup(-1 * time.Second)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if removed != 1 {
		t.Errorf("Cleanup removed = %d, want 1", removed)
	}

	ts.Create(Task{ID: "keep1", Source: "jira", SourceID: "K-1", Title: "Keep", Summary: "s", Status: "pending"})
	removed, _ = ts.Cleanup(-1 * time.Second)
	if removed != 0 {
		t.Errorf("Cleanup removed pending task: %d, want 0", removed)
	}
}

func TestPendingSummary(t *testing.T) {
	store := newTestStore(t)
	ts := NewTaskStore(store)

	ts.Create(Task{ID: "ps1", Source: "jira", SourceID: "BUG-123", Title: "NPE in service", Summary: "s", Status: "pending", Priority: "critical"})
	ts.Create(Task{ID: "ps2", Source: "teams", SourceID: "mention-1", Title: "Deploy question", Summary: "s", Status: "pending", Priority: "normal"})

	summary, err := ts.PendingSummary(10)
	if err != nil {
		t.Fatalf("PendingSummary: %v", err)
	}
	if summary == "" {
		t.Fatal("PendingSummary returned empty string")
	}
	if !strings.Contains(summary, "Pending Tasks (2)") {
		t.Errorf("Summary missing count header: %q", summary)
	}
	if !strings.Contains(summary, "BUG-123") {
		t.Errorf("Summary missing BUG-123: %q", summary)
	}
	if !strings.Contains(summary, "mention-1") {
		t.Errorf("Summary missing mention-1: %q", summary)
	}

	store2 := newTestStore(t)
	ts2 := NewTaskStore(store2)
	empty, _ := ts2.PendingSummary(10)
	if empty != "" {
		t.Errorf("PendingSummary on empty table = %q, want empty", empty)
	}
}

func TestClassifier(t *testing.T) {
	rules := []config.ClassifierRule{
		{Match: config.RuleMatch{Source: "jira", Field: "category", Value: "bug"}, Priority: "high", Action: "queue_review"},
		{Match: config.RuleMatch{Source: "jira", Field: "priority", Value: "Critical"}, Priority: "critical", Action: "auto_save_and_queue"},
		{Match: config.RuleMatch{Source: "email"}, Action: "queue_review"},
		{Match: config.RuleMatch{Source: "teams", Field: "title", Pattern: ".*deploy.*"}, Action: "ignore"},
	}
	c := NewClassifier(rules)

	result := c.Classify(Event{Source: "jira", SourceID: "J-1", Category: "bug"})
	if result.Priority != "high" {
		t.Errorf("Bug priority = %q, want high", result.Priority)
	}
	if result.Action != ActionQueueReview {
		t.Errorf("Bug action = %q, want queue_review", result.Action)
	}

	result = c.Classify(Event{Source: "jira", SourceID: "J-2", RawData: map[string]any{"priority": "Critical"}})
	if result.Priority != "critical" {
		t.Errorf("Critical priority = %q, want critical", result.Priority)
	}
	if result.Action != ActionAutoSaveQueue {
		t.Errorf("Critical action = %q, want auto_save_and_queue", result.Action)
	}

	result = c.Classify(Event{Source: "email", SourceID: "E-1"})
	if result.Action != ActionQueueReview {
		t.Errorf("Email action = %q, want queue_review", result.Action)
	}

	result = c.Classify(Event{Source: "teams", SourceID: "T-1", Title: "Can you deploy staging?"})
	if result.Action != ActionIgnore {
		t.Errorf("Teams deploy action = %q, want ignore", result.Action)
	}

	result = c.Classify(Event{Source: "unknown", SourceID: "U-1"})
	if result.Priority != "normal" {
		t.Errorf("Unmatched priority = %q, want normal", result.Priority)
	}
	if result.Action != ActionQueueReview {
		t.Errorf("Unmatched action = %q, want queue_review", result.Action)
	}
}

func TestWatcherRegistry(t *testing.T) {
	reg := NewWatcherRegistry()
	m1 := &mockWatcher{name: "test1"}
	m2 := &mockWatcher{name: "test2"}
	reg.Register(m1)
	reg.Register(m2)

	all := reg.All()
	if len(all) != 2 {
		t.Errorf("All() = %d, want 2", len(all))
	}

	w, ok := reg.Get("test1")
	if !ok || w.Name() != "test1" {
		t.Errorf("Get(test1) failed")
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Error("Get(nonexistent) returned true")
	}

	reg.CloseAll()
}

func TestSchedulerFullPipeline(t *testing.T) {
	store := newTestStore(t)
	ts := NewTaskStore(store)

	var savedKeys []string
	saveFn := func(key, content, tags string, importance int) error {
		savedKeys = append(savedKeys, key)
		return nil
	}

	rules := []config.ClassifierRule{
		{Match: config.RuleMatch{Source: "jira", Field: "category", Value: "bug"}, Priority: "high", Action: "auto_save_and_queue"},
		{Match: config.RuleMatch{Source: "jira"}, Priority: "normal", Action: "queue_review"},
	}
	classifier := NewClassifier(rules)
	registry := NewWatcherRegistry()

	mock := &mockWatcher{
		name: "jira",
		events: []Event{
			{Source: "jira", SourceID: "BUG-500", Title: "NullPointer in service", Summary: "NPE at line 42", Category: "bug", RawData: map[string]any{"type": "Bug"}},
			{Source: "jira", SourceID: "TASK-999", Title: "Add validation", Summary: "Add input validation", Category: "story"},
		},
	}
	registry.Register(mock)

	scheduler := NewScheduler(1*time.Hour, registry, classifier, ts, saveFn, nil)

	ctx := context.Background()
	scheduler.PollAll(ctx)

	if mock.called != 1 {
		t.Errorf("Watcher polled %d times, want 1", mock.called)
	}

	tasks, err := ts.List(TaskFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("Tasks = %d, want 2", len(tasks))
	}

	var bugTask, storyTask *Task
	for i := range tasks {
		if tasks[i].SourceID == "BUG-500" {
			bugTask = &tasks[i]
		}
		if tasks[i].SourceID == "TASK-999" {
			storyTask = &tasks[i]
		}
	}
	if bugTask == nil || storyTask == nil {
		t.Fatal("Missing expected tasks")
	}
	if bugTask.Priority != "high" {
		t.Errorf("Bug priority = %q, want high", bugTask.Priority)
	}
	if storyTask.Priority != "normal" {
		t.Errorf("Story priority = %q, want normal", storyTask.Priority)
	}

	if len(savedKeys) != 1 || savedKeys[0] != "BUG-500" {
		t.Errorf("Auto-saved keys = %v, want [BUG-500]", savedKeys)
	}

	scheduler.PollAll(ctx)
	tasks, _ = ts.List(TaskFilter{})
	if len(tasks) != 2 {
		t.Errorf("After second run: tasks = %d, want 2 (dedup)", len(tasks))
	}
}

func TestDaemonStartStop(t *testing.T) {
	store := newTestStore(t)
	saveFn := func(key, content, tags string, importance int) error { return nil }

	cfg := config.DaemonConfig{
		Enabled:  true,
		Interval: 3600,
		Cleanup:  config.CleanupConfig{RunOnStart: false},
	}

	d := New(cfg, store, saveFn)
	if d.TaskStore() == nil {
		t.Fatal("TaskStore is nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	cancel()
	d.Stop()
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
