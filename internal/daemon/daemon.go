package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/sivakumar455/memcp/internal/config"
	"github.com/sivakumar455/memcp/internal/memory"
)

// Daemon orchestrates background watchers, classification, and task management.
// Watcher registration is done externally (typically in main.go) via Registry().
type Daemon struct {
	cfg       config.DaemonConfig
	taskStore *TaskStore
	scheduler *Scheduler
	classifier *Classifier
	registry  *WatcherRegistry
}

// New creates a Daemon. Watchers are NOT registered here — call
// d.Registry().Register(w) before d.Start() to add watchers.
func New(cfg config.DaemonConfig, store *memory.Store, saveFn SaveFunc) *Daemon {
	taskStore := NewTaskStore(store)
	registry := NewWatcherRegistry()
	classifier := NewClassifier(cfg.Rules)

	defaultInterval := time.Duration(cfg.Interval) * time.Second
	if defaultInterval < 30*time.Second {
		defaultInterval = 300 * time.Second
	}

	var agent *CursorAgent
	if cfg.CursorAgent.Enabled {
		agent = NewCursorAgent(CursorAgentConfig{
			Enabled:    cfg.CursorAgent.Enabled,
			CursorPath: cfg.CursorAgent.CursorPath,
			Timeout:    cfg.CursorAgent.Timeout,
			WorkDir:    cfg.CursorAgent.WorkDir,
		})
		if agent.Available() {
			slog.Info("CursorAgent enabled for auto_run tasks", "path", agent.cursorPath)
		} else {
			slog.Warn("CursorAgent enabled but cursor CLI not found -- auto_run tasks will be queued only")
			agent = nil
		}
	}

	scheduler := NewScheduler(defaultInterval, registry, classifier, taskStore, saveFn, agent)

	return &Daemon{
		cfg:        cfg,
		taskStore:  taskStore,
		scheduler:  scheduler,
		classifier: classifier,
		registry:   registry,
	}
}

// Registry returns the WatcherRegistry for external watcher registration.
func (d *Daemon) Registry() *WatcherRegistry { return d.registry }

// TaskStore returns the underlying task store.
func (d *Daemon) TaskStore() *TaskStore { return d.taskStore }

// Scheduler returns the scheduler for webhook handler integration.
func (d *Daemon) Scheduler() *Scheduler { return d.scheduler }

// Classifier returns the classifier for webhook handler integration.
func (d *Daemon) Classifier() *Classifier { return d.classifier }

// ResolveInterval returns the watcher-specific interval if > 0, otherwise the daemon default.
func (d *Daemon) ResolveInterval(watcherSeconds int) time.Duration {
	if watcherSeconds > 0 {
		return time.Duration(watcherSeconds) * time.Second
	}
	defaultInterval := time.Duration(d.cfg.Interval) * time.Second
	if defaultInterval < 30*time.Second {
		defaultInterval = 300 * time.Second
	}
	return defaultInterval
}

// Start runs startup cleanup (if configured) and starts the scheduler.
func (d *Daemon) Start(ctx context.Context) error {
	if d.cfg.Cleanup.RunOnStart {
		maxAge := time.Duration(d.cfg.Cleanup.MaxAgeDays) * 24 * time.Hour
		if maxAge <= 0 {
			maxAge = 30 * 24 * time.Hour
		}
		removed, err := d.taskStore.Cleanup(maxAge)
		if err != nil {
			slog.Warn("Startup cleanup failed", "error", err)
		} else if removed > 0 {
			slog.Info("Startup cleanup removed old tasks", "count", removed)
		}
	}

	watchers := d.registry.All()
	if len(watchers) == 0 {
		slog.Warn("No watchers enabled -- daemon will run but idle")
	} else {
		names := make([]string, len(watchers))
		for i, w := range watchers {
			names[i] = w.Name()
		}
		slog.Info("Daemon starting with watchers", "watchers", names)
	}

	go d.scheduler.Run(ctx)

	return nil
}

// Stop closes all registered watchers.
func (d *Daemon) Stop() {
	d.registry.CloseAll()
	slog.Info("Daemon stopped")
}
