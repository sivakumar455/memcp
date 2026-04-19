package gateway

import (
	"encoding/json"
	"net/http"
	"time"
)

func (s *Server) handleMemoryRecall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query   string `json:"query"`
		Session string `json:"session"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	// This assumes ContextBuilder logic inside MemoryManager, which isn't fully mocked for Gateway yet
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"query":   req.Query,
		"context": "Simulated tiered context regarding: " + req.Query,
	})
}

func (s *Server) handleMemorySave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key     string `json:"key"`
		Content string `json:"content"`
		Tags    string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	if req.Key == "" || req.Content == "" {
		writeError(w, http.StatusBadRequest, "missing key or content")
		return
	}

	_, err := s.memMgr.SaveExplicit(req.Key, req.Content, req.Tags, 1, "", "gateway", "gateway")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (s *Server) handleGetPersona(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"identity": "Loaded internally",
		"soul":     "Loaded internally",
	})
}

func (s *Server) handleGetSessions(w http.ResponseWriter, r *http.Request) {
	// POC: Not implemented
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) handleEvolve(w http.ResponseWriter, r *http.Request) {
	// POC: Not implemented externally
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) handleEvolveStatus(w http.ResponseWriter, r *http.Request) {
	lastEvo, err := s.store.GetLastEvolutionTime()
	if err != nil {
		lastEvo = time.Time{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"last_evolution": lastEvo.Format(time.RFC3339),
	})
}
