// Package gateway implements an HTTP API layer for external access to memory and daemon tasks.
package gateway

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/sivakumar455/memcp/internal/config"
	"github.com/sivakumar455/memcp/internal/engine"
	"github.com/sivakumar455/memcp/internal/evolution"
	"github.com/sivakumar455/memcp/internal/memory"
	"github.com/sivakumar455/memcp/internal/persona"
)

// Server is the REST API gateway for memcp.
type Server struct {
	cfg        config.GatewayConfig
	httpServer *http.Server
	mux        *http.ServeMux

	store   *memory.Store
	memMgr  *engine.MemoryManager
	pLoader *persona.Loader
	evoEng  *evolution.Engine
}

// NewServer initializes the HTTP gateway.
func NewServer(cfg config.GatewayConfig, store *memory.Store, memMgr *engine.MemoryManager, pl *persona.Loader, evoEng *evolution.Engine) *Server {
	if cfg.Address == "" {
		cfg.Address = "127.0.0.1:12345"
	}

	s := &Server{
		cfg:     cfg,
		mux:     http.NewServeMux(),
		store:   store,
		memMgr:  memMgr,
		pLoader: pl,
		evoEng:  evoEng,
	}

	s.setupRoutes()

	s.httpServer = &http.Server{
		Addr:    s.cfg.Address,
		Handler: s.mux,
	}

	return s
}

func (s *Server) setupRoutes() {
	// Daemon endpoints
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /tasks", s.handleGetTasks)
	s.mux.HandleFunc("GET /tasks/count", s.handleTasksCount)
	s.mux.HandleFunc("GET /tasks/{id}", s.handleGetTask)
	s.mux.HandleFunc("POST /tasks/{id}/action", s.handleTaskAction)

	// Memory endpoints
	s.mux.HandleFunc("POST /memory/recall", s.handleMemoryRecall)
	s.mux.HandleFunc("POST /memory/save", s.handleMemorySave)
	s.mux.HandleFunc("GET /persona", s.handleGetPersona)
	s.mux.HandleFunc("GET /sessions", s.handleGetSessions)
	s.mux.HandleFunc("POST /evolve", s.handleEvolve)
	s.mux.HandleFunc("GET /evolve/status", s.handleEvolveStatus)
}

// Start runs the HTTP server continuously.
func (s *Server) Start() error {
	slog.Info("starting HTTP server", "address", s.cfg.Address)
	return s.httpServer.ListenAndServe()
}

// Stop halts the HTTP server gracefully.
func (s *Server) Stop(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}
