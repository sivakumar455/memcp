// Package daemon implements background queue polling and task management.
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Event represents an actionable background item found by a watcher.
type Event struct {
	Source   string         `json:"source"`
	SourceID string        `json:"source_id"`
	Title    string         `json:"title"`
	Summary  string         `json:"summary"`
	Category string         `json:"category"`
	RawData  map[string]any `json:"raw_data,omitempty"`
}

// Watcher is an interface for external service pollers.
type Watcher interface {
	Name() string
	Poll(ctx context.Context) ([]Event, error)
	Close() error
}

// WatcherRegistry is a thread-safe registry of watchers.
type WatcherRegistry struct {
	mu       sync.RWMutex
	watchers map[string]Watcher
}

// NewWatcherRegistry creates a new WatcherRegistry.
func NewWatcherRegistry() *WatcherRegistry {
	return &WatcherRegistry{watchers: make(map[string]Watcher)}
}

// Register adds a watcher to the registry.
func (r *WatcherRegistry) Register(w Watcher) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.watchers[w.Name()] = w
	slog.Info("Watcher registered", "name", w.Name())
}

// All returns all registered watchers.
func (r *WatcherRegistry) All() []Watcher {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Watcher, 0, len(r.watchers))
	for _, w := range r.watchers {
		result = append(result, w)
	}
	return result
}

// Get returns a watcher by name.
func (r *WatcherRegistry) Get(name string) (Watcher, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	w, ok := r.watchers[name]
	return w, ok
}

// CloseAll closes all registered watchers.
func (r *WatcherRegistry) CloseAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, w := range r.watchers {
		if err := w.Close(); err != nil {
			slog.Warn("Error closing watcher", "name", name, "error", err)
		}
	}
}

// DummyWatcher is a mock watcher for testing and debugging.
type DummyWatcher struct {
	counter int
}

// NewDummyWatcher creates a mock watcher.
func NewDummyWatcher() *DummyWatcher {
	return &DummyWatcher{}
}

func (d *DummyWatcher) Name() string { return "dummy" }

// Poll generates a dummy event every 3rd call.
func (d *DummyWatcher) Poll(ctx context.Context) ([]Event, error) {
	d.counter++
	if d.counter%3 == 0 {
		return []Event{
			{
				Source:   d.Name(),
				SourceID: fmt.Sprintf("evt-dummy-%d", d.counter),
				Title:    fmt.Sprintf("Dummy Event %d", d.counter),
				Summary:  "This is a mocked daemon event generated for testing.",
				Category: "test",
				RawData:  map[string]any{"mock": true},
			},
		}, nil
	}
	return nil, nil
}

func (d *DummyWatcher) Close() error { return nil }
