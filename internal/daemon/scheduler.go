package daemon

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/sivakumar455/memcp/internal/config"
	"github.com/sivakumar455/memcp/internal/engine"
	"github.com/sivakumar455/memcp/internal/memory"
)

// Scheduler orchestrates periodic polling of Watchers.
type Scheduler struct {
	cfg     config.DaemonConfig
	store   *memory.Store
	memMgr  *engine.MemoryManager
	rules   *Classifier
	
	watchers []Watcher
}

// NewScheduler creates a Scheduler.
func NewScheduler(cfg config.DaemonConfig, store *memory.Store, memMgr *engine.MemoryManager) *Scheduler {
	return &Scheduler{
		cfg:    cfg,
		store:  store,
		memMgr: memMgr,
		rules:  NewClassifier(cfg.Rules),
	}
}

// Register adds a watcher to be scheduled.
func (s *Scheduler) Register(w Watcher) {
	s.watchers = append(s.watchers, w)
}

// Run starts the daemon polling loop until context is canceled.
func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	
	interval := s.cfg.IntervalSecs
	if interval <= 0 {
		interval = 300 // default 5 minutes
	}

	for _, w := range s.watchers {
		wg.Add(1)
		go func(watcher Watcher) {
			defer wg.Done()
			ticker := time.NewTicker(time.Duration(interval) * time.Second)
			defer ticker.Stop()

			// Fire immediately on start
			s.runWatcher(ctx, watcher)

			for {
				select {
				case <-ctx.Done():
					watcher.Close()
					return
				case <-ticker.C:
					s.runWatcher(ctx, watcher)
				}
			}
		}(w)
	}

	wg.Wait()
}

func (s *Scheduler) runWatcher(ctx context.Context, w Watcher) {
	events, err := w.Poll(ctx)
	if err != nil {
		slog.Error("watcher failed", "watcher", w.Name(), "error", err)
		return
	}

	for _, e := range events {
		action, priority := s.rules.Classify(e)
		
		switch action {
		case "ignore", "drop":
			continue
			
		case "auto_save":
			_, err := s.memMgr.SaveExplicit(
				e.SourceID,
				e.Title+"\n\n"+e.Summary,
				"daemon,"+e.Category,
				1,
				"", // no interactive session
				e.Source,
				"daemon",
			)
			if err != nil {
				slog.Error("failed to auto_save event", "event", e.SourceID, "error", err)
			}
			
		case "queue_review", "auto_save_and_queue":
			if action == "auto_save_and_queue" {
				// Save first
				s.memMgr.SaveExplicit(e.SourceID, e.Title+"\n\n"+e.Summary, "daemon,"+e.Category, 1, "", e.Source, "daemon")
			}
			
			// Then Queue for review
			task := &memory.DaemonTask{
				Source:    e.Source,
				SourceID:  e.SourceID,
				Title:     e.Title,
				Summary:   e.Summary,
				Priority:  priority,
				Category:  e.Category,
				Status:    "pending",
				RawData:   e.RawData,
			}
			
			if err := s.store.SaveDaemonTask(task); err != nil {
				slog.Error("failed to save task", "task", e.SourceID, "error", err)
			}
		}
	}
}
