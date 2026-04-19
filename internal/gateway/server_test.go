package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	// Not booting up the full memcp engine, just testing the ServeMux basic bindings
	// It's a bit heavy to start up Store+Engine just for Health handler, so we can test the handler directly.
	
	s := &Server{}
	s.mux = http.NewServeMux()
	s.setupRoutes()
	
	req, _ := http.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	
	s.mux.ServeHTTP(rr, req)
	
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected status %v, got %v", http.StatusOK, status)
	}
	
	var res map[string]string
	json.NewDecoder(rr.Body).Decode(&res)
	if res["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", res["status"])
	}
}

func TestMemorySaveEndpointValidation(t *testing.T) {
	s := &Server{}
	s.mux = http.NewServeMux()
	s.setupRoutes()
	
	reqBody := `{"key": ""}` // Missing content
	req, _ := http.NewRequest("POST", "/memory/save", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	
	s.mux.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("expected status %v, got %v", http.StatusBadRequest, status)
	}
}
