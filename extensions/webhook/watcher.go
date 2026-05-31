// Package webhook implements a generic WebhookWatcher for memcp.
// It starts an HTTP listener that accepts POST requests containing JSON
// payloads matching the daemon.Event schema. This is the simplest possible
// extension and serves as a reference implementation for writing custom watchers.
//
// Example usage in main.go:
//
//	if cfg.Daemon.Watchers.Webhook.Enabled {
//	    wh := webhook.NewWatcher(":9090")
//	    d.Registry().Register(wh)
//	}
package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/sivakumar455/memcp/internal/daemon"
)

// Watcher receives events via HTTP POST and buffers them for the daemon scheduler.
type Watcher struct {
	addr   string
	mu     sync.Mutex
	buffer []daemon.Event
	server *http.Server
}

// NewWatcher creates a WebhookWatcher that listens on the given address.
func NewWatcher(listenAddr string) *Watcher {
	w := &Watcher{addr: listenAddr}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", w.handleEvent)
	mux.HandleFunc("GET /health", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		fmt.Fprintln(rw, `{"status":"ok"}`)
	})

	w.server = &http.Server{Addr: listenAddr, Handler: mux}

	go func() {
		slog.Info("WebhookWatcher HTTP listener starting", "addr", listenAddr)
		if err := w.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("WebhookWatcher listener failed", "error", err)
		}
	}()

	return w
}

func (w *Watcher) Name() string { return "webhook" }

// Poll drains the buffered events and returns them to the scheduler.
func (w *Watcher) Poll(_ context.Context) ([]daemon.Event, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	events := w.buffer
	w.buffer = nil
	return events, nil
}

func (w *Watcher) Close() error {
	if w.server != nil {
		return w.server.Close()
	}
	return nil
}

func (w *Watcher) handleEvent(rw http.ResponseWriter, r *http.Request) {
	var event daemon.Event
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(rw, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if event.Source == "" {
		event.Source = "webhook"
	}

	w.mu.Lock()
	w.buffer = append(w.buffer, event)
	w.mu.Unlock()

	slog.Info("WebhookWatcher received event", "source_id", event.SourceID, "title", event.Title)
	rw.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(rw, `{"status":"accepted","source_id":%q}`, event.SourceID)
}
