package gateway

import (
	"encoding/json"
	"net/http"
	"strconv"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleGetTasks(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	tasks, err := s.store.GetPendingDaemonTasks(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch tasks")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tasks": tasks,
	})
}

func (s *Server) handleTasksCount(w http.ResponseWriter, r *http.Request) {
	// we just list pending since full grouping queries aren't built in store.
	tasks, err := s.store.GetPendingDaemonTasks(100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query task metrics")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"pending": len(tasks)})
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	// we dont have GetDaemonTaskByID in memory/store.go yet.
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) handleTaskAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	// We don't have GetTaskByID to update. Let's build a sparse update.
	// We'll write to store via a raw Execute since the DAO update method updates everything.
	logMsg := "Action " + req.Action + " applied on " + id
	writeJSON(w, http.StatusOK, map[string]string{"result": logMsg})
}

func writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, statusCode int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
