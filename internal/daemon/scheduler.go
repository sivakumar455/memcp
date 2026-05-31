package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SaveFunc is the signature for persisting event data into agent memory.
type SaveFunc func(key, content, tags string, importance int) error

// Scheduler orchestrates periodic polling of Watchers.
type Scheduler struct {
	defaultInterval time.Duration
	intervals       map[string]time.Duration
	mu              sync.RWMutex
	registry        *WatcherRegistry
	classifier      *Classifier
	taskStore       *TaskStore
	saveFn          SaveFunc
	cursorAgent     *CursorAgent
}

// NewScheduler creates a Scheduler that polls watchers and dispatches events.
func NewScheduler(
	defaultInterval time.Duration,
	registry *WatcherRegistry,
	classifier *Classifier,
	taskStore *TaskStore,
	saveFn SaveFunc,
	cursorAgent *CursorAgent,
) *Scheduler {
	return &Scheduler{
		defaultInterval: defaultInterval,
		intervals:       make(map[string]time.Duration),
		registry:        registry,
		classifier:      classifier,
		taskStore:       taskStore,
		saveFn:          saveFn,
		cursorAgent:     cursorAgent,
	}
}

// SetInterval configures a per-watcher polling interval.
func (s *Scheduler) SetInterval(watcherName string, interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.intervals[watcherName] = interval
}

func (s *Scheduler) intervalFor(watcherName string) time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if d, ok := s.intervals[watcherName]; ok && d > 0 {
		return d
	}
	return s.defaultInterval
}

// Run starts a separate goroutine per watcher plus housekeeping.
func (s *Scheduler) Run(ctx context.Context) {
	watchers := s.registry.All()

	slog.Info("Scheduler starting",
		"default_interval", s.defaultInterval,
		"watchers", len(watchers),
	)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.runHousekeeping(ctx)
	}()

	for _, w := range watchers {
		wg.Add(1)
		go func(w Watcher) {
			defer wg.Done()
			s.runWatcher(ctx, w)
		}(w)
	}

	wg.Wait()
	slog.Info("Scheduler shut down")
}

const housekeepingInterval = 60 * time.Second

func (s *Scheduler) runHousekeeping(ctx context.Context) {
	slog.Info("Housekeeping goroutine starting", "interval", housekeepingInterval)
	s.doHousekeeping()

	ticker := time.NewTicker(housekeepingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Housekeeping goroutine shutting down")
			return
		case <-ticker.C:
			s.doHousekeeping()
		}
	}
}

func (s *Scheduler) doHousekeeping() {
	unsnoozed, err := s.taskStore.UnsnoozeReady()
	if err != nil {
		slog.Warn("Unsnooze check failed", "error", err)
	} else if unsnoozed > 0 {
		slog.Info("Unsnoozed tasks", "count", unsnoozed)
	}

	pending, _ := s.taskStore.PendingCount()
	slog.Debug("Housekeeping tick", "pending_tasks", pending)
}

func (s *Scheduler) runWatcher(ctx context.Context, w Watcher) {
	interval := s.intervalFor(w.Name())
	slog.Info("Watcher goroutine starting", "name", w.Name(), "interval", interval)

	s.pollWatcher(ctx, w)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Watcher goroutine shutting down", "name", w.Name())
			return
		case <-ticker.C:
			s.pollWatcher(ctx, w)
		}
	}
}

// PollAll polls every registered watcher once. Used by tests and for one-shot invocations.
func (s *Scheduler) PollAll(ctx context.Context) {
	s.doHousekeeping()
	for _, watcher := range s.registry.All() {
		if ctx.Err() != nil {
			return
		}
		s.pollWatcher(ctx, watcher)
	}
}

func (s *Scheduler) pollWatcher(ctx context.Context, w Watcher) {
	pollCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	slog.Info("Polling watcher", "name", w.Name())
	events, err := w.Poll(pollCtx)
	if err != nil {
		slog.Error("Watcher poll failed", "name", w.Name(), "error", err)
		return
	}

	slog.Info("Watcher returned events", "name", w.Name(), "count", len(events))

	for _, event := range events {
		classified := s.classifier.Classify(event)
		s.HandleClassified(classified)
	}
}

// HandleClassified processes a classified event through the appropriate action.
// Exported so the webhook handler can reuse the same classification pipeline.
func (s *Scheduler) HandleClassified(ce ClassifiedEvent) {
	switch ce.Action {
	case ActionIgnore:
		slog.Debug("Event ignored by classifier", "source", ce.Event.Source, "source_id", ce.Event.SourceID)
		return

	case ActionAutoSave:
		s.autoSave(ce)

	case ActionQueueReview:
		s.queueTask(ce)

	case ActionAutoSaveQueue:
		s.autoSave(ce)
		s.queueTask(ce)

	case ActionAutoRun:
		s.queueTask(ce)
		s.dispatchToCursorAgent(ce)

	default:
		s.queueTask(ce)
	}
}

func (s *Scheduler) autoSave(ce ClassifiedEvent) {
	if s.saveFn == nil {
		return
	}
	tags := ce.Event.Source
	if ce.Event.Category != "" {
		tags += "," + ce.Event.Category
	}
	if err := s.saveFn(ce.Event.SourceID, ce.Event.Summary, tags, 1); err != nil {
		slog.Error("Auto-save failed", "source_id", ce.Event.SourceID, "error", err)
	} else {
		slog.Info("Auto-saved event to memory", "source_id", ce.Event.SourceID)
	}
}

func (s *Scheduler) queueTask(ce ClassifiedEvent) {
	exists, err := s.taskStore.Exists(ce.Event.Source, ce.Event.SourceID)
	if err != nil {
		slog.Warn("Dedup check failed, queuing anyway", "source_id", ce.Event.SourceID, "error", err)
	} else if exists {
		slog.Debug("Task already exists, skipping", "source", ce.Event.Source, "source_id", ce.Event.SourceID)
		return
	}

	rawJSON := ""
	if ce.Event.RawData != nil {
		b, _ := json.Marshal(ce.Event.RawData)
		rawJSON = string(b)
	}

	task := Task{
		ID:       uuid.New().String(),
		Source:   ce.Event.Source,
		SourceID: ce.Event.SourceID,
		Title:    ce.Event.Title,
		Summary:  ce.Event.Summary,
		Priority: ce.Priority,
		Category: ce.Event.Category,
		Status:   "pending",
		RawData:  rawJSON,
	}

	if err := s.taskStore.Create(task); err != nil {
		slog.Error("Failed to queue task", "source_id", ce.Event.SourceID, "error", err)
	} else {
		slog.Info("Task queued", "id", task.ID, "source_id", task.SourceID, "priority", task.Priority)
		if ce.Priority == "critical" || ce.Priority == "high" {
			sendDesktopNotification(ce.Priority, ce.Event.SourceID, ce.Event.Title)
		}
	}
}

func (s *Scheduler) dispatchToCursorAgent(ce ClassifiedEvent) {
	if s.cursorAgent == nil || !s.cursorAgent.Available() {
		slog.Debug("CursorAgent not available, skipping auto_run", "source_id", ce.Event.SourceID)
		return
	}

	tasks, err := s.taskStore.List(TaskFilter{Source: ce.Event.Source, Limit: 1})
	if err != nil || len(tasks) == 0 {
		return
	}
	var task *Task
	for i := range tasks {
		if tasks[i].SourceID == ce.Event.SourceID {
			task = &tasks[i]
			break
		}
	}
	if task == nil {
		return
	}

	go func(t Task) {
		slog.Info("CursorAgent dispatching task", "source_id", t.SourceID, "title", t.Title)
		_ = s.taskStore.UpdateStatus(t.ID, "in_progress", "")

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		result, err := s.cursorAgent.RunTask(ctx, t)
		if err != nil {
			slog.Error("CursorAgent task failed", "source_id", t.SourceID, "error", err)
			_ = s.taskStore.UpdateStatus(t.ID, "pending", "cursor agent failed: "+err.Error())
			sendDesktopNotification("high", t.SourceID, "Auto-analysis failed, queued for manual review")
			return
		}

		if s.saveFn != nil {
			tags := t.Source + ",auto_run"
			if t.Category != "" {
				tags += "," + t.Category
			}
			content := fmt.Sprintf("Auto-analysis by cursor agent:\n\n%s", result)
			_ = s.saveFn(t.SourceID+"-analysis", content, tags, 1)
		}

		_ = s.taskStore.UpdateStatus(t.ID, "completed", result)
		sendDesktopNotification("normal", t.SourceID, "Auto-analysis complete")
		slog.Info("CursorAgent task completed", "source_id", t.SourceID, "result_len", len(result))
	}(*task)
}

func sendDesktopNotification(priority, sourceID, title string) {
	if runtime.GOOS != "darwin" {
		return
	}
	msg := fmt.Sprintf("[%s] %s: %s", priority, sourceID, title)
	script := fmt.Sprintf(`display notification %q with title "memcp daemon" subtitle "New %s task"`, msg, priority)
	cmd := exec.Command("osascript", "-e", script)
	if err := cmd.Start(); err != nil {
		slog.Debug("Desktop notification failed", "error", err)
	}
}
