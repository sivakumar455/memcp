// Package daemon implements background queue polling and task management.
package daemon

import (
	"context"
	"fmt"
)

// Event represents an actionable background item found by a watcher.
type Event struct {
	Source   string
	SourceID string
	Title    string
	Summary  string
	Category string
	Priority string // optional, if the watcher knows it implicitly
	RawData  string
}

// Watcher is an interface for external service pollers.
type Watcher interface {
	Name() string
	Poll(ctx context.Context) ([]Event, error)
	Close() error
}

// DummyWatcher is a mock watcher for testing and debugging.
type DummyWatcher struct {
	counter int
}

// NewDummyWatcher creates a mock watcher.
func NewDummyWatcher() *DummyWatcher {
	return &DummyWatcher{}
}

// Name returns the watcher's identifier.
func (d *DummyWatcher) Name() string {
	return "dummy"
}

// Poll generates a dummy event every 3rd call.
func (d *DummyWatcher) Poll(ctx context.Context) ([]Event, error) {
	d.counter++
	
	// Create a bit of noise but don't overwhelm
	if d.counter%3 == 0 {
		return []Event{
			{
				Source:   d.Name(),
				SourceID: fmt.Sprintf("evt-dummy-%d", d.counter),
				Title:    fmt.Sprintf("Dummy Event %d", d.counter),
				Summary:  "This is a mocked daemon event generated for testing.",
				Category: "test",
				Priority: "low",
				RawData:  `{"mock": true}`,
			},
		}, nil
	}
	
	return nil, nil // normal idle poll
}

// Close cleans up watcher resources.
func (d *DummyWatcher) Close() error {
	return nil
}
